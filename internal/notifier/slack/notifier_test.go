package slack_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/port"
	"github.com/RikuShimoida/job-hunt-agent/internal/notifier/slack"
)

// recorder は Webhook が受け取ったリクエストを記録する。
type recorder struct {
	mu sync.Mutex

	bodies      []string
	contentType string
	status      int
}

func (r *recorder) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		r.mu.Lock()
		r.bodies = append(r.bodies, string(body))
		r.contentType = req.Header.Get("Content-Type")
		status := r.status
		r.mu.Unlock()

		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
	}
}

func (r *recorder) requestCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bodies)
}

func (r *recorder) texts(t *testing.T) []string {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]string, 0, len(r.bodies))
	for _, body := range r.bodies {
		var p struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatalf("受信ボディが JSON として読めない: %v (%s)", err, body)
		}
		out = append(out, p.Text)
	}
	return out
}

func job(id int64, title string, score int) model.JobPosting {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	rateMin, rateMax := 750000, 850000
	days := 3

	return model.JobPosting{
		ID:               id,
		Title:            title,
		Score:            score,
		RateType:         model.RateTypeMonthly,
		RateMin:          &rateMin,
		RateMax:          &rateMax,
		WorkDaysMin:      &days,
		WorkDaysMax:      &days,
		RemoteType:       model.RemoteTypeFullRemote,
		Location:         "東京",
		StartDate:        &start,
		RequiredSkills:   []string{"Java", "Spring", "AWS"},
		SourceURL:        "https://example.test/jobs/1",
		ScoreReasons:     []string{"希望単価以上", "フルリモート"},
		RejectionReasons: []string{"Terraform実務経験が歓迎条件"},
		Sources:          []model.JobSource{{SourceName: "レバテック"}},
	}
}

// TestNotifyPostsJobToWebhook は、案件が Webhook へ POST され、
// 受入条件が求める全項目が本文に含まれることを確かめる。
func TestNotifyPostsJobToWebhook(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	n := slack.New(srv.URL)

	records, err := n.Notify(context.Background(),
		[]port.NotifyItem{{Job: job(1, "Java／AWS 基盤改善案件", 92)}})
	if err != nil {
		t.Fatalf("Notify() returned error: %v", err)
	}

	if rec.requestCount() != 1 {
		t.Fatalf("POST 回数 = %d, want 1", rec.requestCount())
	}
	if rec.contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", rec.contentType)
	}

	text := rec.texts(t)[0]
	required := []string{
		"92点",
		"新着",
		"Java／AWS 基盤改善案件",
		"750000〜850000円",
		"週3日",
		"2026-09-01",
		"フルリモート",
		"東京",
		"Java、Spring、AWS",
		"レバテック",
		"希望単価以上",
		"Terraform実務経験が歓迎条件",
		"https://example.test/jobs/1",
	}
	for _, want := range required {
		if !strings.Contains(text, want) {
			t.Errorf("送信本文に %q が含まれていない\n--- 本文 ---\n%s", want, text)
		}
	}

	if len(records) != 1 {
		t.Fatalf("送信記録 = %d件, want 1件", len(records))
	}
	if records[0].Result != model.NotificationResultSuccess {
		t.Errorf("Result = %q, want success", records[0].Result)
	}
	if records[0].JobID != 1 {
		t.Errorf("JobID = %d, want 1", records[0].JobID)
	}
	if records[0].Channel != "slack" {
		t.Errorf("Channel = %q, want slack", records[0].Channel)
	}
	if records[0].PayloadHash == "" {
		t.Error("PayloadHash が空（通知済み管理に使えない）")
	}
	if records[0].SentAt.IsZero() {
		t.Error("SentAt が未設定")
	}
}

func TestNotifyMarksUpdate(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	n := slack.New(srv.URL)

	if _, err := n.Notify(context.Background(),
		[]port.NotifyItem{{Job: job(1, "案件", 92), Update: true}}); err != nil {
		t.Fatalf("Notify() returned error: %v", err)
	}

	if text := rec.texts(t)[0]; !strings.Contains(text, "【92点・更新】") {
		t.Errorf("更新として送られていない: %q", text)
	}
}

// TestNotifySendsNothingWhenNoTargets は、通知対象が0件のときに
// HTTP リクエストが1件も飛ばない（無音である）ことを確かめる。
func TestNotifySendsNothingWhenNoTargets(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	n := slack.New(srv.URL)

	records, err := n.Notify(context.Background(), nil)
	if err != nil {
		t.Fatalf("Notify() returned error: %v", err)
	}

	if rec.requestCount() != 0 {
		t.Errorf("0件なのに POST が %d回飛んだ, want 0回", rec.requestCount())
	}
	if len(records) != 0 {
		t.Errorf("送信記録 = %+v, want なし", records)
	}
}

// TestNotifyRecordsFailureOnServerError は、Slack が 500 を返したときに
// 成功として記録しないことを確かめる（次回実行で再送されるため）。
func TestNotifyRecordsFailureOnServerError(t *testing.T) {
	t.Parallel()

	rec := &recorder{status: http.StatusInternalServerError}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	n := slack.New(srv.URL)

	records, err := n.Notify(context.Background(),
		[]port.NotifyItem{{Job: job(1, "案件", 92)}})
	if err != nil {
		t.Fatalf("Notify() returned error: %v（送信失敗で全体を落としてはならない）", err)
	}

	if len(records) != 1 {
		t.Fatalf("送信記録 = %d件, want 1件", len(records))
	}
	if records[0].Result != model.NotificationResultFailed {
		t.Errorf("Result = %q, want failed", records[0].Result)
	}
	if !strings.Contains(records[0].ErrorMessage, "500") {
		t.Errorf("ErrorMessage = %q, want ステータスコードを含む", records[0].ErrorMessage)
	}
}

// TestNotifyContinuesAfterFailure は、1件失敗しても残りが送られることを確かめる。
func TestNotifyContinuesAfterFailure(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		calls int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()

		if n == 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// 送信間隔はこのテストの関心事ではない。実時間を待たせないため 0 にする。
	n := slack.New(srv.URL, slack.WithSendInterval(0))

	records, err := n.Notify(context.Background(), []port.NotifyItem{
		{Job: job(1, "案件A", 92)},
		{Job: job(2, "案件B", 88)},
		{Job: job(3, "案件C", 70)},
	})
	if err != nil {
		t.Fatalf("Notify() returned error: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("送信記録 = %d件, want 3件（失敗で後続が止まっている）", len(records))
	}

	want := []model.NotificationResult{
		model.NotificationResultSuccess,
		model.NotificationResultFailed,
		model.NotificationResultSuccess,
	}
	for i, w := range want {
		if records[i].Result != w {
			t.Errorf("%d件目の Result = %q, want %q", i+1, records[i].Result, w)
		}
	}
}

// TestNotifyDoesNotLeakWebhookURL は、送信エラーに Webhook URL が
// 混ざらないことを確かめる。
//
// net/http の送信エラーは *url.Error であり、Error() が URL 全体を含む。
// これをそのままラップすると、ログと DB（notifications.error_message /
// collection_runs.error_message）へ秘密情報が書き出される。
func TestNotifyDoesNotLeakWebhookURL(t *testing.T) {
	t.Parallel()

	// 接続を受け付けないポートを指すことで *url.Error を発生させる。
	const secret = "T00000000-B11111111-SUPERSECRETTOKEN"
	webhookURL := "http://127.0.0.1:1/services/" + secret

	n := slack.New(webhookURL)

	records, err := n.Notify(context.Background(),
		[]port.NotifyItem{{Job: job(1, "案件", 92)}})
	if err != nil {
		t.Fatalf("Notify() returned error: %v", err)
	}
	if len(records) != 1 || records[0].Result != model.NotificationResultFailed {
		t.Fatalf("送信記録 = %+v, want failed が1件", records)
	}

	msg := records[0].ErrorMessage
	if strings.Contains(msg, secret) || strings.Contains(msg, webhookURL) {
		t.Errorf("エラーメッセージに Webhook URL が漏れている: %q", msg)
	}
	if msg == "" {
		t.Error("エラーメッセージが空（原因を特定できない）")
	}
}

func TestNotifyErrorIsIdentifiableBySentinel(t *testing.T) {
	t.Parallel()

	rec := &recorder{status: http.StatusInternalServerError}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	n := slack.NewErrorNotifier(srv.URL)

	err := n.NotifyError(context.Background(),
		[]model.SourceFailure{{SourceName: "fixture-email", Message: "failed to read dir"}})
	if !errors.Is(err, slack.ErrSend) {
		t.Errorf("err = %v, want slack.ErrSend でラップされていること", err)
	}
}

// TestNotifyErrorPostsFailuresToSeparateWebhook は、ソース取得失敗が
// 案件通知とは別の Webhook へ1回だけ送られることを確かめる。
func TestNotifyErrorPostsFailuresToSeparateWebhook(t *testing.T) {
	t.Parallel()

	jobRec := &recorder{}
	jobSrv := httptest.NewServer(jobRec.handler())
	defer jobSrv.Close()

	errRec := &recorder{}
	errSrv := httptest.NewServer(errRec.handler())
	defer errSrv.Close()

	errNotifier := slack.NewErrorNotifier(errSrv.URL)

	failures := []model.SourceFailure{
		{SourceName: "fixture-email", Message: "failed to read fixture dir"},
		{SourceName: "fixture-html", Message: "connection refused"},
	}
	if err := errNotifier.NotifyError(context.Background(), failures); err != nil {
		t.Fatalf("NotifyError() returned error: %v", err)
	}

	if errRec.requestCount() != 1 {
		t.Fatalf("エラー Webhook への POST 回数 = %d, want 1（失敗はまとめて1回）",
			errRec.requestCount())
	}
	if jobRec.requestCount() != 0 {
		t.Errorf("案件通知の Webhook へ %d回飛んだ, want 0回（別チャンネルであるべき）",
			jobRec.requestCount())
	}

	text := errRec.texts(t)[0]
	for _, want := range []string{"収集エラー", "fixture-email", "fixture-html"} {
		if !strings.Contains(text, want) {
			t.Errorf("エラー通知の本文に %q が含まれていない: %q", want, text)
		}
	}
}

func TestNotifyErrorSendsNothingWithoutFailures(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	n := slack.NewErrorNotifier(srv.URL)

	if err := n.NotifyError(context.Background(), nil); err != nil {
		t.Fatalf("NotifyError() returned error: %v", err)
	}
	if rec.requestCount() != 0 {
		t.Errorf("失敗が無いのに POST が %d回飛んだ, want 0回", rec.requestCount())
	}
}

// TestNotifyWaitsBetweenSends は、案件を続けて送るときに間隔が空くことを確かめる。
//
// Slack の Incoming Webhook は秒間1メッセージを超えると 429 を返すため、
// 初回収集のように通知が並ぶと後半が落ちる。
func TestNotifyWaitsBetweenSends(t *testing.T) {
	t.Parallel()

	const interval = 200 * time.Millisecond

	rec := &recorder{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	n := slack.New(srv.URL, slack.WithSendInterval(interval))

	start := time.Now()
	records, err := n.Notify(context.Background(), []port.NotifyItem{
		{Job: job(1, "案件A", 92)},
		{Job: job(2, "案件B", 88)},
		{Job: job(3, "案件C", 70)},
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Notify() returned error: %v", err)
	}
	if len(records) != 3 || rec.requestCount() != 3 {
		t.Fatalf("送信記録 %d件 / POST %d回, want どちらも3", len(records), rec.requestCount())
	}

	// 3件なら間隔は2回ぶん空く。
	if want := 2 * interval; elapsed < want {
		t.Errorf("所要時間 = %v, want %v 以上（送信間隔が空いていない）", elapsed, want)
	}
}

// TestNotifyDoesNotWaitAfterLastSend は、最後の送信のあとに待たないことを確かめる。
// 待っても 429 は避けられず、実行時間が伸びるだけになる。
func TestNotifyDoesNotWaitAfterLastSend(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	// 最後に待つ実装なら10秒かかる。
	n := slack.New(srv.URL, slack.WithSendInterval(10*time.Second))

	start := time.Now()
	if _, err := n.Notify(context.Background(),
		[]port.NotifyItem{{Job: job(1, "案件", 92)}}); err != nil {
		t.Fatalf("Notify() returned error: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Errorf("1件の送信に %v かかった（最後の送信後にウェイトしている）", elapsed)
	}
	if rec.requestCount() != 1 {
		t.Errorf("POST 回数 = %d, want 1", rec.requestCount())
	}
}

// TestNotifyCancelDuringWaitReturnsImmediately は、送信間隔の待機中に
// 中断されたら即座に抜けることを確かめる（Ctrl-C がウェイトぶん効かないと困る）。
func TestNotifyCancelDuringWaitReturnsImmediately(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	// time.Sleep で待つ実装なら10秒ぶら下がる。
	n := slack.New(srv.URL, slack.WithSendInterval(10*time.Second))

	// 1件目の POST（ローカルの httptest 宛でミリ秒オーダー）は終わり、
	// 2件目の前のウェイトに入っているタイミングで中断する。
	time.AfterFunc(250*time.Millisecond, cancel)

	start := time.Now()
	records, err := n.Notify(ctx, []port.NotifyItem{
		{Job: job(1, "案件A", 92)},
		{Job: job(2, "案件B", 88)},
	})
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled でラップされていること", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("中断までに %v かかった（ウェイトが ctx のキャンセルで抜けていない）", elapsed)
	}

	// 送信できた1件は記録として返す（呼び出し側が通知済みとして確定させる）。
	if len(records) != 1 || records[0].Result != model.NotificationResultSuccess {
		t.Fatalf("送信記録 = %+v, want success が1件", records)
	}
	if rec.requestCount() != 1 {
		t.Errorf("POST 回数 = %d, want 1（中断後も送信している）", rec.requestCount())
	}
}

// TestNotifyStopsOnCanceledContext は、context のキャンセルで送信を中断し、
// 未送信の案件が通知済みにならないことを確かめる。
func TestNotifyStopsOnCanceledContext(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	n := slack.New(srv.URL)

	records, err := n.Notify(ctx, []port.NotifyItem{{Job: job(1, "案件", 92)}})
	if err == nil {
		t.Fatal("キャンセル済み context でエラーが返らなかった")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled でラップされていること", err)
	}
	if rec.requestCount() != 0 {
		t.Errorf("キャンセル済みなのに POST が %d回飛んだ, want 0回", rec.requestCount())
	}
	if len(records) != 0 {
		t.Errorf("送信記録 = %+v, want なし（送っていない案件を記録しない）", records)
	}
}
