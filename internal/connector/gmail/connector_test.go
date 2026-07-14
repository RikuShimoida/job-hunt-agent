package gmail_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/RikuShimoida/job-hunt-agent/internal/connector/gmail"
)

// testToken はテスト用のアクセストークン。Authorization ヘッダの検証に使う。
const testToken = "test-access-token"

func tokenSource() oauth2.TokenSource {
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: testToken, TokenType: "Bearer"})
}

// listCall は messages.list へ来た1回ぶんのリクエスト。
type listCall struct {
	query      string
	maxResults string
	pageToken  string
}

// stub は Gmail API のスタブ。
type stub struct {
	// pages は list が返すメッセージ ID のページ。最後以外は nextPageToken を付ける。
	pages []([]string)
	// bodies は ID → text/plain の本文。
	bodies map[string]string
	// failGet はこの ID の messages.get を 500 で落とす。
	failGet map[string]bool

	listCalls []listCall
	getCalls  []string
	authSeen  []string
}

func (s *stub) server(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/gmail/v1/users/me/messages", func(w http.ResponseWriter, r *http.Request) {
		s.authSeen = append(s.authSeen, r.Header.Get("Authorization"))
		q := r.URL.Query()
		s.listCalls = append(s.listCalls, listCall{
			query:      q.Get("q"),
			maxResults: q.Get("maxResults"),
			pageToken:  q.Get("pageToken"),
		})

		page := 0
		if tok := q.Get("pageToken"); tok != "" {
			var err error
			page, err = strconv.Atoi(tok)
			if err != nil {
				http.Error(w, "bad page token", http.StatusBadRequest)
				return
			}
		}
		if page >= len(s.pages) {
			writeJSON(t, w, map[string]any{})
			return
		}

		ids := make([]map[string]string, 0, len(s.pages[page]))
		for _, id := range s.pages[page] {
			ids = append(ids, map[string]string{"id": id})
		}
		res := map[string]any{"messages": ids}
		if page+1 < len(s.pages) {
			res["nextPageToken"] = strconv.Itoa(page + 1)
		}
		writeJSON(t, w, res)
	})

	mux.HandleFunc("/gmail/v1/users/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		s.authSeen = append(s.authSeen, r.Header.Get("Authorization"))
		id := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/messages/")
		s.getCalls = append(s.getCalls, id)

		if s.failGet[id] {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		body, ok := s.bodies[id]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		writeJSON(t, w, map[string]any{
			"id":           id,
			"internalDate": "1783000000000",
			"payload": map[string]any{
				"mimeType": "multipart/alternative",
				"headers": []map[string]string{
					{"name": "From", "value": "agent@example-agent.test"},
					{"name": "Subject", "value": "案件情報"},
				},
				"parts": []map[string]any{
					{
						"mimeType": "text/html",
						"body":     map[string]string{"data": encode("<html>無視されるべき HTML</html>")},
					},
					{
						"mimeType": "text/plain",
						"body":     map[string]string{"data": encode(body)},
					},
				},
			},
		})
	})

	return httptest.NewServer(mux)
}

// newConnector はスタブへ向けたコネクタを返す。
//
// *http.Client ごと差し替えず Transport だけを渡すのは、oauth2 のラップ
// （Authorization の付与）を迂回させないため。
func newConnector(t *testing.T, srv *httptest.Server, senders []string, newerThan string, maxResults int) *gmail.Connector {
	t.Helper()

	return gmail.New("gmail-agents", senders, newerThan, maxResults, tokenSource(),
		gmail.WithEndpoint(srv.URL),
		gmail.WithTransport(srv.Client().Transport),
	)
}

func TestFetch(t *testing.T) {
	t.Parallel()

	s := &stub{
		pages:  [][]string{{"msg-1", "msg-2"}},
		bodies: map[string]string{"msg-1": "案件A の本文", "msg-2": "案件B の本文"},
	}
	srv := s.server(t)
	defer srv.Close()

	c := newConnector(t, srv, []string{"a@example.test", "b@example.test"}, "30d", 100)

	jobs, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("取得件数 = %d, want 2", len(jobs))
	}

	// 送信元ホワイトリストで検索していること。キーワード検索にすると
	// 転職サイトの求人メールがノイズとして大量に混ざる。
	wantQuery := "from:(a@example.test OR b@example.test) newer_than:30d"
	if s.listCalls[0].query != wantQuery {
		t.Errorf("query = %q, want %q", s.listCalls[0].query, wantQuery)
	}

	// Transport を差し替えても oauth2 のラップを迂回せず、必ず認証が付くこと。
	if len(s.authSeen) == 0 {
		t.Fatal("リクエストが記録されていない")
	}
	for _, got := range s.authSeen {
		if want := "Bearer " + testToken; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
	}

	job := jobs[0]
	if job.SourceName != "gmail-agents" {
		t.Errorf("SourceName = %q, want %q", job.SourceName, "gmail-agents")
	}
	if job.Format != "email" {
		t.Errorf("Format = %q, want %q", job.Format, "email")
	}
	if job.ExternalID != "msg-1" {
		t.Errorf("ExternalID = %q, want %q", job.ExternalID, "msg-1")
	}
	if job.Sender != "agent@example-agent.test" {
		t.Errorf("Sender = %q, want %q", job.Sender, "agent@example-agent.test")
	}
	// text/html ではなく text/plain パートを採ること。
	if job.Body != "案件A の本文" {
		t.Errorf("Body = %q, want %q", job.Body, "案件A の本文")
	}
	if job.ReceivedAt == nil {
		t.Fatal("ReceivedAt = nil, want 値あり")
	}
	if want := time.UnixMilli(1783000000000).UTC(); !job.ReceivedAt.Equal(want) {
		t.Errorf("ReceivedAt = %v, want %v", job.ReceivedAt, want)
	}
}

// TestFetchStopsAtMaxResults は max_results を超えて取得しないことを固定する。
func TestFetchStopsAtMaxResults(t *testing.T) {
	t.Parallel()

	s := &stub{
		// サーバーが上限を無視して3件返してきても、取得は2件に留めること。
		pages:  [][]string{{"msg-1", "msg-2", "msg-3"}},
		bodies: map[string]string{"msg-1": "本文1", "msg-2": "本文2", "msg-3": "本文3"},
	}
	srv := s.server(t)
	defer srv.Close()

	c := newConnector(t, srv, []string{"a@example.test"}, "7d", 2)

	jobs, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if got := s.listCalls[0].maxResults; got != "2" {
		t.Errorf("maxResults = %q, want %q", got, "2")
	}
	if len(jobs) > 2 {
		t.Errorf("取得件数 = %d, want 2以下", len(jobs))
	}
	if !strings.Contains(s.listCalls[0].query, "newer_than:7d") {
		t.Errorf("query = %q, want newer_than:7d を含む", s.listCalls[0].query)
	}
}

// TestFetchFollowsPagination は nextPageToken を辿って全件取得することを固定する。
func TestFetchFollowsPagination(t *testing.T) {
	t.Parallel()

	s := &stub{
		pages:  [][]string{{"msg-1", "msg-2"}, {"msg-3"}},
		bodies: map[string]string{"msg-1": "本文1", "msg-2": "本文2", "msg-3": "本文3"},
	}
	srv := s.server(t)
	defer srv.Close()

	c := newConnector(t, srv, []string{"a@example.test"}, "30d", 100)

	jobs, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("取得件数 = %d, want 3（2ページ目まで辿ること）", len(jobs))
	}
	if len(s.listCalls) != 2 {
		t.Fatalf("messages.list の呼び出し = %d回, want 2回", len(s.listCalls))
	}
	if s.listCalls[1].pageToken != "1" {
		t.Errorf("2回目の pageToken = %q, want %q", s.listCalls[1].pageToken, "1")
	}
}

// TestFetchContinuesAfterSingleMessageFailure は、1通の取得に失敗しても
// 残りの案件を捨てないことを固定する（architecture §6）。
//
// 1通の欠損で取得済みの案件まで捨てると、たまたま壊れたメールが1通あるだけで
// その日の収集が丸ごと無音になる。
func TestFetchContinuesAfterSingleMessageFailure(t *testing.T) {
	t.Parallel()

	s := &stub{
		pages:   [][]string{{"msg-1", "msg-broken", "msg-3"}},
		bodies:  map[string]string{"msg-1": "本文1", "msg-3": "本文3"},
		failGet: map[string]bool{"msg-broken": true},
	}
	srv := s.server(t)
	defer srv.Close()

	c := newConnector(t, srv, []string{"a@example.test"}, "30d", 100)

	jobs, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v, want nil（1通失敗しても残りは返す）", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("取得件数 = %d, want 2（壊れた1通を除く）", len(jobs))
	}
}

// TestFetchFailsWhenAllMessagesFail は、全通が失敗したときは
// 黙って0件成功にせずエラーを返すことを固定する（認証切れ・レート制限の可能性）。
func TestFetchFailsWhenAllMessagesFail(t *testing.T) {
	t.Parallel()

	s := &stub{
		pages:   [][]string{{"msg-1", "msg-2"}},
		bodies:  map[string]string{},
		failGet: map[string]bool{"msg-1": true, "msg-2": true},
	}
	srv := s.server(t)
	defer srv.Close()

	c := newConnector(t, srv, []string{"a@example.test"}, "30d", 100)

	_, err := c.Fetch(context.Background())
	if err == nil {
		t.Fatal("Fetch() error = nil, want エラー（全通失敗を0件成功にしない）")
	}
	if !errors.Is(err, gmail.ErrFetch) {
		t.Errorf("errors.Is(err, ErrFetch) = false, err = %v", err)
	}
}

func TestFetchRespectsCanceledContext(t *testing.T) {
	t.Parallel()

	s := &stub{pages: [][]string{{"msg-1"}}, bodies: map[string]string{"msg-1": "本文"}}
	srv := s.server(t)
	defer srv.Close()

	c := newConnector(t, srv, []string{"a@example.test"}, "30d", 100)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.Fetch(ctx)
	if err == nil {
		t.Fatal("Fetch() error = nil, want エラー（キャンセル済み context）")
	}
	if !errors.Is(err, gmail.ErrFetch) {
		t.Errorf("errors.Is(err, ErrFetch) = false, err = %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(err, context.Canceled) = false, err = %v", err)
	}
}

// TestFetchErrorDoesNotLeakURL は、取得エラーの文言に Gmail のエンドポイント URL や
// 送信元アドレスが混ざらないことを固定する。
//
// collect_jobs.go はコネクタのエラーを err.Error() でログへ出し、
// collection_runs.error_message へも永続化する。net/http のエラーは *url.Error で
// URL 全体を含むため、素通しにすると検索クエリ（= 監視対象の送信元アドレス）が
// ログと DB に残る。
func TestFetchErrorDoesNotLeakURL(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newConnector(t, srv, []string{"secret-sender@example.test"}, "30d", 100)

	_, err := c.Fetch(context.Background())
	if err == nil {
		t.Fatal("Fetch() error = nil, want エラー")
	}
	if !errors.Is(err, gmail.ErrFetch) {
		t.Errorf("errors.Is(err, ErrFetch) = false, err = %v", err)
	}
	if strings.Contains(err.Error(), srv.URL) {
		t.Errorf("エラー文言にエンドポイント URL が含まれる: %v", err)
	}
	if strings.Contains(err.Error(), "secret-sender@example.test") {
		t.Errorf("エラー文言に送信元アドレスが含まれる: %v", err)
	}
}

func TestFetchUnauthorizedGuidesToReauth(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newConnector(t, srv, []string{"a@example.test"}, "30d", 100)

	_, err := c.Fetch(context.Background())
	if err == nil {
		t.Fatal("Fetch() error = nil, want エラー")
	}
	if !strings.Contains(err.Error(), "auth gmail") {
		t.Errorf("401 のエラーが再認証の手順を案内していない: %v", err)
	}
}

// TestFetchWithoutTokenSourceFails は、トークン供給元が無いまま取得しようとしたら
// 無認証で Gmail を叩かずエラーになることを固定する。
func TestFetchWithoutTokenSourceFails(t *testing.T) {
	t.Parallel()

	c := gmail.New("gmail-agents", []string{"a@example.test"}, "30d", 100, nil)

	_, err := c.Fetch(context.Background())
	if !errors.Is(err, gmail.ErrFetch) {
		t.Errorf("errors.Is(err, ErrFetch) = false, err = %v", err)
	}
}

func encode(s string) string {
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(s))
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("スタブのレスポンス書き込みに失敗: %v", err)
	}
}
