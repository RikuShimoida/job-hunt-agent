package cli

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	assets "github.com/RikuShimoida/job-hunt-agent"
	"github.com/RikuShimoida/job-hunt-agent/internal/application"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

// TestInitWritesFilesIndependentOfWorkingDir は、init がひな形を埋め込みから読み、
// リポジトリ外の作業ディレクトリでも設定を生成できることを固定する。
//
// 以前はひな形をカレントディレクトリ相対で読んでいたため、go install したバイナリを
// 別ディレクトリで叩くと失敗した。埋め込みにしたことでその前提が消えたことを担保する。
func TestInitWritesFilesIndependentOfWorkingDir(t *testing.T) {
	// t.Chdir は t.Parallel と併用できない（プロセス全体の CWD を変えるため）。
	t.Chdir(t.TempDir())

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err != nil {
		t.Fatalf("init Execute error: %v", err)
	}

	for _, path := range []string{
		filepath.Join("config", "profile.yaml"),
		filepath.Join("config", "sources.yaml"),
		".env",
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("init did not create %s: %v", path, err)
		}
	}

	// 生成物が埋め込んだひな形と一致すること（空ファイルを書いていないことの担保）。
	want, err := fs.ReadFile(assets.FS, "config/profile.example.yaml")
	if err != nil {
		t.Fatalf("failed to read embedded example: %v", err)
	}
	got, err := os.ReadFile(filepath.Join("config", "profile.yaml"))
	if err != nil {
		t.Fatalf("failed to read generated profile: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("generated profile.yaml does not match embedded example")
	}
}

// TestReportNotify は、送信失敗が終了コードへ出ることを確かめる。
//
// notify / run の RunE がこの関数の戻り値をそのまま返し、main が
// 非 nil のエラーで os.Exit(1) する。失敗を握り潰すと、定期実行が
// 成功扱いで終わり、通知が届いていないことに気づけない。
func TestReportNotify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		summary application.NotifySummary
		wantErr bool
	}{
		{
			name:    "通知対象が0件なら正常終了する",
			summary: application.NotifySummary{},
			wantErr: false,
		},
		{
			name:    "全件送信できたら正常終了する",
			summary: application.NotifySummary{TargetCount: 2, SentCount: 2},
			wantErr: false,
		},
		{
			name:    "1件でも送信に失敗したらエラーを返す",
			summary: application.NotifySummary{TargetCount: 3, SentCount: 2, FailedCount: 1},
			wantErr: true,
		},
		{
			name:    "全件失敗したらエラーを返す",
			summary: application.NotifySummary{TargetCount: 2, FailedCount: 2},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			err := reportNotify(&out, tt.summary)

			if tt.wantErr && !errors.Is(err, ErrNotifyFailed) {
				t.Errorf("err = %v, want ErrNotifyFailed でラップされていること", err)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("err = %v, want nil", err)
			}

			// 失敗時もサマリは出す（何件届いたのかが分からないと再実行の判断ができない）。
			if got := out.String(); !strings.Contains(got, "通知対象") {
				t.Errorf("サマリが出力されていない: %q", got)
			}
		})
	}
}

// TestProfileApplyRequiresFrom は --from 未指定を ErrProfileApply で弾くことを確かめる。
func TestProfileApplyRequiresFrom(t *testing.T) {
	t.Parallel()

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"profile", "apply"})

	if err := root.Execute(); !errors.Is(err, ErrProfileApply) {
		t.Fatalf("err = %v, want wrapped ErrProfileApply", err)
	}
}

// TestProfileApplyEndToEnd は apply が profile を保存し、history が一覧できることを確かめる。
func TestProfileApplyEndToEnd(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.yaml")
	fromPath := filepath.Join(dir, "proposed.yaml")

	const oldBody = "search_status: watching\nrequired_skills:\n  - Java\n"
	const newBody = "search_status: searching\nrequired_skills:\n  - Go\n"
	if err := os.WriteFile(profilePath, []byte(oldBody), 0o600); err != nil {
		t.Fatalf("failed to seed profile: %v", err)
	}
	if err := os.WriteFile(fromPath, []byte(newBody), 0o600); err != nil {
		t.Fatalf("failed to write proposed: %v", err)
	}

	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--profile", profilePath, "profile", "apply", "--from", fromPath})
	if err := root.Execute(); err != nil {
		t.Fatalf("apply Execute error: %v", err)
	}

	got, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("failed to read profile after apply: %v", err)
	}
	if string(got) != newBody {
		t.Errorf("profile after apply = %q, want %q", got, newBody)
	}

	// history が退避を1件一覧できる。
	root = NewRootCommand()
	var histOut bytes.Buffer
	root.SetOut(&histOut)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--profile", profilePath, "profile", "history"})
	if err := root.Execute(); err != nil {
		t.Fatalf("history Execute error: %v", err)
	}
	if strings.TrimSpace(histOut.String()) == "" || strings.Contains(histOut.String(), "履歴はありません") {
		t.Errorf("history output = %q, want one entry", histOut.String())
	}
}

// TestProfileApplyRejectsInvalid は不正な profile を弾き、現行を変えないことを確かめる。
func TestProfileApplyRejectsInvalid(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.yaml")
	fromPath := filepath.Join(dir, "proposed.yaml")

	const oldBody = "search_status: searching\nrequired_skills:\n  - Java\n"
	const invalid = "search_status: searching\nrequired_skills:\n  - Go\nremote_required: true\nmax_onsite_days: 2\n"
	if err := os.WriteFile(profilePath, []byte(oldBody), 0o600); err != nil {
		t.Fatalf("failed to seed profile: %v", err)
	}
	if err := os.WriteFile(fromPath, []byte(invalid), 0o600); err != nil {
		t.Fatalf("failed to write proposed: %v", err)
	}

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--profile", profilePath, "profile", "apply", "--from", fromPath})
	if err := root.Execute(); !errors.Is(err, model.ErrInvalidProfile) {
		t.Fatalf("err = %v, want wrapped model.ErrInvalidProfile", err)
	}

	got, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("failed to read profile: %v", err)
	}
	if string(got) != oldBody {
		t.Errorf("profile changed to %q, want unchanged %q", got, oldBody)
	}
}
