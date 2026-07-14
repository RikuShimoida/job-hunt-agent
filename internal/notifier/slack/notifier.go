// Package slack は Slack Incoming Webhook へ通知を送る Notifier。
//
// slack-go/slack を使わないのは、あれが Web API 向けの重い依存であり、
// Webhook へ JSON を1本 POST するだけのために入れる理由がないため。
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/port"
	"github.com/RikuShimoida/job-hunt-agent/internal/notifier/message"
)

// ErrSend は Slack への送信が失敗したことを示す。
var ErrSend = errors.New("slack send failed")

// defaultTimeout は Webhook 1本ぶんの上限。ctx のキャンセルとは別に、
// 応答が返らないまま定期実行がぶら下がり続けるのを防ぐ。
const defaultTimeout = 10 * time.Second

// defaultSendInterval は案件を1件送るごとに空ける間隔。
// Slack の Incoming Webhook は秒間1メッセージを超えると 429 を返すため、
// 初回収集のように通知が10件以上並ぶと後半が落ちる。
const defaultSendInterval = time.Second

// webhook は Incoming Webhook 1本への POST 口。
type webhook struct {
	url    string
	client *http.Client
}

func newWebhook(rawURL string) *webhook {
	return &webhook{
		url:    rawURL,
		client: &http.Client{Timeout: defaultTimeout},
	}
}

type payload struct {
	Text string `json:"text"`
}

func (w *webhook) post(ctx context.Context, text string) error {
	body, err := json.Marshal(payload{Text: text})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSend, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%w: invalid webhook request", ErrSend)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSend, stripURL(err))
	}
	defer resp.Body.Close() //nolint:errcheck // 読み終えた body の close 失敗に打つ手はない

	// レスポンスボディは読まない。Slack はエラー時に本文を返すが、
	// それをログや DB へ流すと Webhook URL を含む情報が混ざる余地を残すため。
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: status %d", ErrSend, resp.StatusCode)
	}
	return nil
}

// stripURL は *url.Error から URL を剥がす。
//
// net/http の送信エラーは *url.Error であり、Error() が Webhook URL 全体を含む。
// そのままラップすると、上位のログと collection_runs.error_message /
// notifications.error_message へ秘密情報が書き出されるため、原因だけを取り出す。
func stripURL(err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return uerr.Err
	}
	return err
}

// Notifier は案件通知を Slack へ送る。
type Notifier struct {
	wh       *webhook
	now      func() time.Time
	interval time.Duration
}

// Option は Notifier の任意設定。
type Option func(*Notifier)

// WithSendInterval は案件1件ごとのウェイトを差し替える。0 以下ならウェイトしない。
// テストを実時間で待たせないために、間隔を外から与えられるようにしている。
func WithSendInterval(d time.Duration) Option {
	return func(n *Notifier) { n.interval = d }
}

func New(webhookURL string, opts ...Option) *Notifier {
	n := &Notifier{
		wh:       newWebhook(webhookURL),
		now:      time.Now,
		interval: defaultSendInterval,
	}
	for _, opt := range opts {
		opt(n)
	}
	return n
}

func (n *Notifier) Name() string { return "slack" }

// wait は次の送信までウェイトする。
// time.Sleep にしないのは、中断しても最大1秒ぶら下がり続けるため。
func (n *Notifier) wait(ctx context.Context) error {
	if n.interval <= 0 {
		return nil
	}

	timer := time.NewTimer(n.interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return fmt.Errorf("notify canceled: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

// Notify は案件を1件ずつ送り、成功・失敗の記録を返す。
//
// 1件の失敗で全体を止めないのは、成功した案件まで次回へ持ち越すと
// 同じ案件が何度も再送され、通知がノイズになるため。
func (n *Notifier) Notify(ctx context.Context, items []port.NotifyItem) ([]model.Notification, error) {
	if len(items) == 0 {
		return nil, nil
	}

	records := make([]model.Notification, 0, len(items))
	for i, item := range items {
		if err := ctx.Err(); err != nil {
			return records, fmt.Errorf("notify canceled: %w", err)
		}

		// 待つのは2件目以降だけ。最後の送信のあとに待っても 429 は避けられず、
		// 実行時間が伸びるだけになる。
		if i > 0 {
			if err := n.wait(ctx); err != nil {
				return records, err
			}
		}

		rec := model.Notification{
			JobID:          item.Job.ID,
			Channel:        n.Name(),
			SentAt:         n.now(),
			PayloadHash:    model.MaterialHash(item.Job),
			MaterialFields: message.Snapshot(item.Job),
			Result:         model.NotificationResultSuccess,
		}
		if err := n.wh.post(ctx, message.Format(item)); err != nil {
			rec.Result = model.NotificationResultFailed
			rec.ErrorMessage = err.Error()
		}
		records = append(records, rec)
	}
	return records, nil
}

// ErrorNotifier はソース取得失敗を Slack へ送る。案件通知とは別の Webhook を使う。
type ErrorNotifier struct {
	wh *webhook
}

func NewErrorNotifier(webhookURL string) *ErrorNotifier {
	return &ErrorNotifier{wh: newWebhook(webhookURL)}
}

// NotifyError は失敗をまとめて1回だけ送る。
// 失敗ごとに送ると、ソースが軒並み落ちたときに通知が埋まるため。
func (n *ErrorNotifier) NotifyError(ctx context.Context, failures []model.SourceFailure) error {
	if len(failures) == 0 {
		return nil
	}
	return n.wh.post(ctx, message.FormatFailures(failures))
}
