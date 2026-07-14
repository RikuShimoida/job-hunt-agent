package gmail_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RikuShimoida/job-hunt-agent/internal/connector/gmail"
)

// recorder は Gmail API へ飛んだリクエストを記録するスタブ。
type recorder struct {
	queries []string
	fetched []string
}

func newStubServer(t *testing.T, rec *recorder, bodies map[string]string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/gmail/v1/users/me/messages", func(w http.ResponseWriter, r *http.Request) {
		rec.queries = append(rec.queries, r.URL.Query().Get("q"))

		ids := make([]map[string]string, 0, len(bodies))
		for id := range bodies {
			ids = append(ids, map[string]string{"id": id})
		}
		// map の反復順は不定なので、テストの安定のため ID 順に並べる。
		sortByID(ids)

		writeJSON(t, w, map[string]any{"messages": ids})
	})

	mux.HandleFunc("/gmail/v1/users/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/messages/")
		body, ok := bodies[id]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		rec.fetched = append(rec.fetched, id)

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

func TestFetch(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := newStubServer(t, rec, map[string]string{
		"msg-1": "案件A の本文",
		"msg-2": "案件B の本文",
	})
	defer srv.Close()

	c := gmail.New("gmail-agents",
		[]string{"a@example.test", "b@example.test"},
		"30d", 100, nil,
		gmail.WithEndpoint(srv.URL),
		gmail.WithHTTPClient(srv.Client()),
	)

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
	if rec.queries[0] != wantQuery {
		t.Errorf("query = %q, want %q", rec.queries[0], wantQuery)
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

func TestFetchRespectsMaxResults(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := newStubServer(t, rec, map[string]string{"msg-1": "本文"})
	defer srv.Close()

	c := gmail.New("gmail-agents", []string{"a@example.test"}, "7d", 1, nil,
		gmail.WithEndpoint(srv.URL),
		gmail.WithHTTPClient(srv.Client()),
	)

	if _, err := c.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if !strings.Contains(rec.queries[0], "newer_than:7d") {
		t.Errorf("query = %q, want newer_than:7d を含む", rec.queries[0])
	}
}

func TestFetchRespectsCanceledContext(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := newStubServer(t, rec, map[string]string{"msg-1": "本文"})
	defer srv.Close()

	c := gmail.New("gmail-agents", []string{"a@example.test"}, "30d", 100, nil,
		gmail.WithEndpoint(srv.URL),
		gmail.WithHTTPClient(srv.Client()),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.Fetch(ctx); err == nil {
		t.Fatal("Fetch() error = nil, want エラー（キャンセル済み context）")
	}
}

// TestFetchErrorDoesNotLeakURL は、取得エラーの文言に Gmail のエンドポイント URL が
// 混ざらないことを固定する。
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

	c := gmail.New("gmail-agents", []string{"secret-sender@example.test"}, "30d", 100, nil,
		gmail.WithEndpoint(srv.URL),
		gmail.WithHTTPClient(srv.Client()),
	)

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

	c := gmail.New("gmail-agents", []string{"a@example.test"}, "30d", 100, nil,
		gmail.WithEndpoint(srv.URL),
		gmail.WithHTTPClient(srv.Client()),
	)

	_, err := c.Fetch(context.Background())
	if err == nil {
		t.Fatal("Fetch() error = nil, want エラー")
	}
	if !strings.Contains(err.Error(), "auth gmail") {
		t.Errorf("401 のエラーが再認証の手順を案内していない: %v", err)
	}
}

func encode(s string) string {
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(s))
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("スタブのレスポンス書き込みに失敗: %v", err)
	}
}

func sortByID(ids []map[string]string) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j]["id"] < ids[j-1]["id"]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}
