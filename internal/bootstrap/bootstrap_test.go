package bootstrap_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/RikuShimoida/job-hunt-agent/internal/bootstrap"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

// newOptions は example 設定を使う組み立て入力を返す。
// sources.example.yaml の path がリポジトリ相対のため、作業ディレクトリを移す。
func newOptions(t *testing.T, dryRun bool) bootstrap.Options {
	t.Helper()

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	t.Chdir(root)
	t.Setenv("DATABASE_URL", filepath.Join(t.TempDir(), "test.db"))

	return bootstrap.Options{
		ProfilePath: filepath.Join(root, "config", "profile.example.yaml"),
		SourcesPath: filepath.Join(root, "config", "sources.example.yaml"),
		DryRun:      dryRun,
		Out:         &bytes.Buffer{},
		LogOut:      &bytes.Buffer{},
	}
}

// TestNewFailsWithoutWebhookURL は、実送信で Webhook が未設定のとき
// センチネルエラーで停止することを確かめる。
//
// 標準出力へ黙ってフォールバックすると「送ったつもりで送られていない」事故になる。
func TestNewFailsWithoutWebhookURL(t *testing.T) {
	opts := newOptions(t, false)
	t.Setenv("SLACK_WEBHOOK_URL", "")

	app, err := bootstrap.New(context.Background(), opts)
	if err == nil {
		if cerr := app.Close(); cerr != nil {
			t.Errorf("failed to close app: %v", cerr)
		}
		t.Fatal("SLACK_WEBHOOK_URL 未設定なのにエラーが返らなかった")
	}
	if !errors.Is(err, model.ErrMissingWebhookURL) {
		t.Errorf("err = %v, want model.ErrMissingWebhookURL でラップされていること", err)
	}
}

// TestNewSucceedsWithWebhookURL は、Webhook が設定されていれば実送信で組み立てられることを確かめる。
func TestNewSucceedsWithWebhookURL(t *testing.T) {
	opts := newOptions(t, false)
	t.Setenv("SLACK_WEBHOOK_URL", "https://hooks.example.test/services/dummy")

	app, err := bootstrap.New(context.Background(), opts)
	if err != nil {
		t.Fatalf("bootstrap.New() returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Errorf("failed to close app: %v", err)
		}
	})
}

// TestNewDryRunDoesNotRequireWebhookURL は、--dry-run なら Webhook 未設定でも動くことを確かめる。
func TestNewDryRunDoesNotRequireWebhookURL(t *testing.T) {
	opts := newOptions(t, true)
	t.Setenv("SLACK_WEBHOOK_URL", "")

	app, err := bootstrap.New(context.Background(), opts)
	if err != nil {
		t.Fatalf("dry-run で bootstrap.New() がエラーを返した: %v", err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Errorf("failed to close app: %v", err)
		}
	})
}
