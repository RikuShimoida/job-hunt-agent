// Package gmail は Gmail から案件メールを読み取るコネクタ。
//
// google.golang.org/api/gmail/v1 を使わないのは、必要なのが messages.list と
// messages.get の2エンドポイントだけであり、grpc を含む依存ツリーを引き込む対価に
// 見合わないため（slack-go を却下して Webhook を net/http で叩いたのと同じ判断）。
// トークンの更新だけは自前実装が無意味なので golang.org/x/oauth2 に任せる。
//
// スコープは gmail.readonly のみ。削除・アーカイブ・既読化・ラベル変更・返信は行わない。
package gmail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

// ErrFetch は Gmail からの取得が失敗したことを示す。
var ErrFetch = errors.New("gmail fetch failed")

// ScopeReadonly は要求する唯一のスコープ。
const ScopeReadonly = "https://www.googleapis.com/auth/gmail.readonly"

// Endpoint は Google の OAuth2 エンドポイント。
//
// golang.org/x/oauth2/google の google.Endpoint を使わないのは、あの
// パッケージが GCE のメタデータサーバー検出のために
// cloud.google.com/go/compute/metadata を引き込むため。ここで必要なのは
// URL 2本だけであり、依存を1つ増やす理由がない。
var Endpoint = oauth2.Endpoint{
	AuthURL:   "https://accounts.google.com/o/oauth2/auth",
	TokenURL:  "https://oauth2.googleapis.com/token",
	AuthStyle: oauth2.AuthStyleInParams,
}

const (
	defaultEndpoint = "https://gmail.googleapis.com"
	defaultTimeout  = 30 * time.Second
	// Gmail の messages.list が1回に返す上限。
	maxPageSize = 100
	// 1レスポンスから読む最大バイト数（Gmail のメッセージ上限 25MB に余裕を持たせる）。
	maxResponseBytes = 32 << 20
)

// Connector は送信元を絞って案件メールを取得する。
type Connector struct {
	name        string
	senders     []string
	newerThan   string
	maxResults  int
	tokenSource oauth2.TokenSource

	endpoint  string
	transport http.RoundTripper
}

// Option はコネクタの差し替え可能な部分を設定する。
type Option func(*Connector)

// WithEndpoint は Gmail API のエンドポイントを差し替える（テスト用）。
func WithEndpoint(endpoint string) Option {
	return func(c *Connector) { c.endpoint = endpoint }
}

// WithTransport は HTTP の往路だけを差し替える（テスト用）。
//
// *http.Client ごと差し替えないのは、それだと oauth2.NewClient のラップまで消え、
// 本番から誤って渡したときに Authorization ヘッダの無い素のクライアントで
// Gmail を叩いてしまうため。Transport だけを差し替えれば認証は必ず経由する。
func WithTransport(rt http.RoundTripper) Option {
	return func(c *Connector) { c.transport = rt }
}

// New は Gmail コネクタを返す。
func New(name string, senders []string, newerThan string, maxResults int, ts oauth2.TokenSource, opts ...Option) *Connector {
	c := &Connector{
		name:        name,
		senders:     senders,
		newerThan:   newerThan,
		maxResults:  maxResults,
		tokenSource: ts,
		endpoint:    defaultEndpoint,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Connector) Name() string { return c.name }

// Fetch は送信元を絞って案件メールを取得する。
//
// 1通の取得に失敗しても残りは処理を続ける（architecture §6「1件の解析に失敗しても
// そのソースの他の案件は処理を続ける」）。1通の欠損で取得済みの案件まで捨てると、
// たまたま壊れたメールが1通あるだけでその日の収集が丸ごと無音になる。
// 全通が失敗した場合だけ、ソースの失敗としてエラーを返す。
func (c *Connector) Fetch(ctx context.Context) ([]model.RawJob, error) {
	client, err := c.httpClient(ctx)
	if err != nil {
		return nil, err
	}

	ids, err := c.listMessageIDs(ctx, client)
	if err != nil {
		return nil, err
	}

	var (
		jobs     = make([]model.RawJob, 0, len(ids))
		firstErr error
		failed   int
	)
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("%w: canceled: %w", ErrFetch, err)
		}
		msg, err := c.getMessage(ctx, client, id)
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		jobs = append(jobs, c.toRawJob(msg))
	}

	// 1通も取れなかったのに ID は引けている場合、認証切れやレート制限など
	// ソース全体の問題である可能性が高い。黙って0件成功にしない。
	if len(ids) > 0 && failed == len(ids) {
		return nil, fmt.Errorf("%w: all %d messages failed: %w", ErrFetch, failed, firstErr)
	}
	return jobs, nil
}

// httpClient は必ず oauth2 のラップを通したクライアントを返す。
// Transport の差し替え（テスト）を挟んでも Authorization の付与は迂回できない。
func (c *Connector) httpClient(ctx context.Context) (*http.Client, error) {
	if c.tokenSource == nil {
		return nil, fmt.Errorf("%w: token source is not configured", ErrFetch)
	}
	base := &http.Client{Transport: c.transport}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, base)

	client := oauth2.NewClient(ctx, c.tokenSource)
	client.Timeout = defaultTimeout
	return client, nil
}

// query は送信元ホワイトリストで検索クエリを組む。
func (c *Connector) query() string {
	quoted := make([]string, 0, len(c.senders))
	for _, s := range c.senders {
		if s = strings.TrimSpace(s); s != "" {
			quoted = append(quoted, s)
		}
	}
	q := "from:(" + strings.Join(quoted, " OR ") + ")"
	if c.newerThan != "" {
		q += " newer_than:" + c.newerThan
	}
	return q
}

func (c *Connector) listMessageIDs(ctx context.Context, client *http.Client) ([]string, error) {
	var (
		ids       []string
		pageToken string
	)

	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("%w: canceled: %w", ErrFetch, err)
		}

		remaining := c.maxResults - len(ids)
		if remaining <= 0 {
			break
		}
		pageSize := min(remaining, maxPageSize)

		params := url.Values{}
		params.Set("q", c.query())
		params.Set("maxResults", strconv.Itoa(pageSize))
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}

		var res listResponse
		endpoint := c.endpoint + "/gmail/v1/users/me/messages?" + params.Encode()
		if err := c.getJSON(ctx, client, endpoint, &res); err != nil {
			return nil, err
		}

		// maxResults はサーバーへも渡すが、返ってきた件数でも切る。
		// API が上限を超えて返しても取得が膨らまないようにする。
		for _, m := range res.Messages {
			if len(ids) >= c.maxResults {
				break
			}
			ids = append(ids, m.ID)
		}
		if res.NextPageToken == "" || len(res.Messages) == 0 {
			break
		}
		pageToken = res.NextPageToken
	}
	return ids, nil
}

func (c *Connector) getMessage(ctx context.Context, client *http.Client, id string) (message, error) {
	var msg message
	endpoint := c.endpoint + "/gmail/v1/users/me/messages/" + url.PathEscape(id) + "?format=full"
	if err := c.getJSON(ctx, client, endpoint, &msg); err != nil {
		return message{}, err
	}
	return msg, nil
}

// getJSON は GET して JSON をデコードする。
//
// エラーから *url.Error を剥がすのは、Error() がリクエスト URL 全体を含み、
// 呼び出し元（collect_jobs.go）がそれをログと collection_runs.error_message へ
// 書き出すため。アクセストークンはヘッダなので URL には出ないが、
// クエリに送信元アドレスが載るため受信箱の中身がログに残る。
func (c *Connector) getJSON(ctx context.Context, client *http.Client, endpoint string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("%w: failed to build request", ErrFetch)
	}

	resp, err := client.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			return fmt.Errorf("%w: %v", ErrFetch, uerr.Err)
		}
		return fmt.Errorf("%w: %v", ErrFetch, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// レスポンスボディを出さないのは、Google のエラーが
		// リクエスト内容を反射することがあるため。
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("%w: status %d (認証に失敗しました。job-hunt-agent auth gmail でトークンを取り直してください)",
				ErrFetch, resp.StatusCode)
		}
		return fmt.Errorf("%w: status %d", ErrFetch, resp.StatusCode)
	}

	// 上限を課すのは、壊れた応答や巨大な添付を含むメールでメモリを際限なく
	// 食わないため。Gmail のメッセージ1通の上限（25MB）に少し余裕を持たせる。
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("%w: failed to read response", ErrFetch)
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("%w: failed to decode response", ErrFetch)
	}
	return nil
}

func (c *Connector) toRawJob(msg message) model.RawJob {
	raw := model.RawJob{
		SourceName: c.name,
		Body:       msg.body(),
		Format:     "email",
		ExternalID: msg.ID,
		Sender:     msg.header("From"),
	}
	if at, ok := msg.receivedAt(); ok {
		raw.ReceivedAt = &at
	}
	return raw
}

type listResponse struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
	NextPageToken string `json:"nextPageToken"`
}

type message struct {
	ID           string  `json:"id"`
	InternalDate string  `json:"internalDate"`
	Payload      payload `json:"payload"`
}

type payload struct {
	MimeType string    `json:"mimeType"`
	Headers  []header  `json:"headers"`
	Body     partBody  `json:"body"`
	Parts    []payload `json:"parts"`
}

type header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type partBody struct {
	Data string `json:"data"`
}

func (m message) header(name string) string {
	for _, h := range m.Payload.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

// body は text/plain パートを優先して取り出す。
//
// text/html を先に見ないのは、HTML にはレイアウト用のテーブルとインライン CSS が
// 大量に含まれ、行ベースのラベル抽出が成立しないため。対象2社はどちらも
// text/plain パートを持つ。
func (m message) body() string {
	if s, ok := findPart(m.Payload, "text/plain"); ok {
		return s
	}
	if s, ok := findPart(m.Payload, "text/html"); ok {
		return s
	}
	return decodeBase64URL(m.Payload.Body.Data)
}

func findPart(p payload, mimeType string) (string, bool) {
	if strings.EqualFold(p.MimeType, mimeType) && p.Body.Data != "" {
		if s := decodeBase64URL(p.Body.Data); s != "" {
			return s, true
		}
	}
	for _, child := range p.Parts {
		if s, ok := findPart(child, mimeType); ok {
			return s, true
		}
	}
	return "", false
}

func decodeBase64URL(s string) string {
	if s == "" {
		return ""
	}
	b, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(strings.TrimRight(s, "="))
	if err != nil {
		return ""
	}
	return string(b)
}

// receivedAt は internalDate（epoch ミリ秒）を時刻へ直す。
func (m message) receivedAt() (time.Time, bool) {
	if m.InternalDate == "" {
		return time.Time{}, false
	}
	ms, err := strconv.ParseInt(m.InternalDate, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.UnixMilli(ms).UTC(), true
}
