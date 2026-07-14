//go:build integration

// 実 Gmail へ接続して案件メールを取得できることを確かめる。
//
// 実行手順:
//  1. Google Cloud Console で Gmail API を有効化し、OAuth クライアント ID
//     （種類: デスクトップアプリ）を発行する
//  2. `job-hunt-agent auth gmail` でリフレッシュトークンを取得する
//  3. GOOGLE_CLIENT_ID / GOOGLE_CLIENT_SECRET / GOOGLE_REFRESH_TOKEN を環境変数へ設定する
//  4. go test -tags=integration ./internal/connector/gmail/
//
// 資格情報が未設定ならスキップする（CI で赤くしないため）。
package gmail_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/RikuShimoida/job-hunt-agent/internal/connector/gmail"
)

func TestFetchFromRealGmail(t *testing.T) {
	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	refreshToken := os.Getenv("GOOGLE_REFRESH_TOKEN")

	if clientID == "" || clientSecret == "" || refreshToken == "" {
		t.Skip("GOOGLE_CLIENT_ID / GOOGLE_CLIENT_SECRET / GOOGLE_REFRESH_TOKEN が未設定のためスキップ")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     gmail.Endpoint,
		Scopes:       []string{gmail.ScopeReadonly},
	}
	ts := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})

	c := gmail.New("gmail-agents",
		[]string{
			"alliance-crowdtech@crowdworks.co.jp",
			"careers.desk.haishin@foster-net.co.jp",
		},
		"30d", 10, ts,
	)

	jobs, err := c.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(jobs) == 0 {
		t.Fatal("取得件数 = 0。直近30日に対象エージェントからのメールが無いか、検索クエリが誤っている")
	}

	for _, job := range jobs {
		if job.Format != "email" {
			t.Errorf("Format = %q, want %q", job.Format, "email")
		}
		if job.ExternalID == "" {
			t.Error("ExternalID が空（Gmail のメッセージ ID が取れていない）")
		}
		if job.Sender == "" {
			t.Error("Sender が空（From ヘッダが取れていない）")
		}
		if strings.TrimSpace(job.Body) == "" {
			t.Errorf("Body が空（text/plain パートが取れていない）: id=%s", job.ExternalID)
		}
		if job.ReceivedAt == nil {
			t.Errorf("ReceivedAt が nil（internalDate が取れていない）: id=%s", job.ExternalID)
		}
	}

	t.Logf("取得件数 = %d", len(jobs))
}
