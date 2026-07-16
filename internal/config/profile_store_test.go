package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

const validProfileYAML = `search_status: searching
required_skills:
  - Go
`

func writeProfile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("failed to write profile fixture: %v", err)
	}
}

func TestApplyProfileBacksUpBeforeWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.yaml")
	historyDir := filepath.Join(dir, "profile.history")

	const oldBody = "search_status: watching\nrequired_skills:\n  - Java\n"
	writeProfile(t, profilePath, oldBody)

	now := time.Date(2026, 7, 15, 12, 30, 45, 0, time.UTC)
	result, err := ApplyProfile(profilePath, historyDir, []byte(validProfileYAML), now)
	if err != nil {
		t.Fatalf("ApplyProfile returned error: %v", err)
	}

	if result.BackupPath == "" {
		t.Fatal("BackupPath is empty, want a backup to be created")
	}
	backup, err := os.ReadFile(result.BackupPath)
	if err != nil {
		t.Fatalf("failed to read backup: %v", err)
	}
	if string(backup) != oldBody {
		t.Errorf("backup content = %q, want the previous profile %q", backup, oldBody)
	}

	// 退避ファイル名は now 由来のタイムスタンプを持つ。
	wantName := "profile-20260715T123045Z.yaml"
	if got := filepath.Base(result.BackupPath); got != wantName {
		t.Errorf("backup file name = %q, want %q", got, wantName)
	}

	current, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("failed to read profile after apply: %v", err)
	}
	if string(current) != validProfileYAML {
		t.Errorf("profile after apply = %q, want the proposed body %q", current, validProfileYAML)
	}
}

func TestApplyProfileRejectsInvalidAndKeepsCurrent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		proposed string
	}{
		{
			name:     "remote_required と max_onsite_days の同時指定",
			proposed: "search_status: searching\nrequired_skills:\n  - Go\nremote_required: true\nmax_onsite_days: 2\n",
		},
		{
			name:     "minimum_rate が負値",
			proposed: "search_status: searching\nrequired_skills:\n  - Go\nminimum_rate: -1\n",
		},
		{
			name:     "search_status が不正",
			proposed: "search_status: bogus\nrequired_skills:\n  - Go\n",
		},
		{
			name:     "required_skills が空",
			proposed: "search_status: searching\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			profilePath := filepath.Join(dir, "profile.yaml")
			historyDir := filepath.Join(dir, "profile.history")

			const oldBody = "search_status: searching\nrequired_skills:\n  - Java\n"
			writeProfile(t, profilePath, oldBody)

			now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
			_, err := ApplyProfile(profilePath, historyDir, []byte(tt.proposed), now)
			if !errors.Is(err, model.ErrInvalidProfile) {
				t.Fatalf("err = %v, want wrapped model.ErrInvalidProfile", err)
			}

			// 検証失敗時は現行 profile.yaml を一切変更しない。
			current, err := os.ReadFile(profilePath)
			if err != nil {
				t.Fatalf("failed to read profile: %v", err)
			}
			if string(current) != oldBody {
				t.Errorf("profile changed to %q, want unchanged %q", current, oldBody)
			}

			// 検証失敗時は履歴も残さない（失敗した提案で履歴を汚さない）。
			if _, err := os.Stat(historyDir); !os.IsNotExist(err) {
				t.Errorf("history dir exists after validation failure, want none (stat err = %v)", err)
			}
		})
	}
}

func TestApplyProfileRejectsUnknownKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.yaml")
	historyDir := filepath.Join(dir, "profile.history")

	const oldBody = "search_status: searching\nrequired_skills:\n  - Java\n"
	writeProfile(t, profilePath, oldBody)

	// `remote_requird` はタイポ（正: remote_required）。非 strict だと黙って無視され
	// 検証を通過してしまうため、apply では ErrInvalidProfile で弾く。
	const typo = "search_status: searching\nrequired_skills:\n  - Go\nremote_requird: true\n"
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	_, err := ApplyProfile(profilePath, historyDir, []byte(typo), now)
	if !errors.Is(err, model.ErrInvalidProfile) {
		t.Fatalf("err = %v, want wrapped model.ErrInvalidProfile for unknown key", err)
	}

	current, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("failed to read profile: %v", err)
	}
	if string(current) != oldBody {
		t.Errorf("profile changed to %q, want unchanged %q", current, oldBody)
	}
}

func TestApplyProfileInitialCreate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.yaml")
	historyDir := filepath.Join(dir, "profile.history")

	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	result, err := ApplyProfile(profilePath, historyDir, []byte(validProfileYAML), now)
	if err != nil {
		t.Fatalf("ApplyProfile returned error: %v", err)
	}

	if result.BackupPath != "" {
		t.Errorf("BackupPath = %q, want empty on initial create", result.BackupPath)
	}
	current, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("failed to read created profile: %v", err)
	}
	if string(current) != validProfileYAML {
		t.Errorf("created profile = %q, want %q", current, validProfileYAML)
	}
}

func TestApplyProfileVerbatimKeepsComments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.yaml")
	historyDir := filepath.Join(dir, "profile.history")

	// スキル別名（k8s）とコメントを含む提案が、正規化・再マーシャルされずそのまま保存されること。
	const proposed = "# 求職状態\nsearch_status: searching\nrequired_skills:\n  - k8s\n"
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	if _, err := ApplyProfile(profilePath, historyDir, []byte(proposed), now); err != nil {
		t.Fatalf("ApplyProfile returned error: %v", err)
	}

	current, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("failed to read profile: %v", err)
	}
	if string(current) != proposed {
		t.Errorf("profile = %q, want verbatim %q (comments/aliases preserved)", current, proposed)
	}
}

// TestApplyProfileAcceptsExampleStrict は、記入例の全キーが構造体に対応していることを
// strict デコード経由で保証する（example.yaml と model.Profile のドリフト検知）。
// これが落ちると、記入例を土台にした apply が実利用者の手元でも strict に弾かれる。
func TestApplyProfileAcceptsExampleStrict(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile(filepath.Join("..", "..", "config", "profile.example.yaml"))
	if err != nil {
		t.Fatalf("failed to read example profile: %v", err)
	}

	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.yaml")
	historyDir := filepath.Join(dir, "profile.history")
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	if _, err := ApplyProfile(profilePath, historyDir, body, now); err != nil {
		t.Fatalf("ApplyProfile rejected the example profile under strict decode: %v", err)
	}
}

func TestListHistoryNewestFirst(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.yaml")
	historyDir := filepath.Join(dir, "profile.history")

	writeProfile(t, profilePath, "search_status: searching\nrequired_skills:\n  - v1\n")

	// 3回 apply すると、2回ぶんの履歴（v1, v2）が退避される。
	stamps := []time.Time{
		time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 15, 11, 0, 0, 0, time.UTC),
	}
	bodies := []string{
		"search_status: searching\nrequired_skills:\n  - v2\n",
		"search_status: searching\nrequired_skills:\n  - v3\n",
	}
	for i := range stamps {
		if _, err := ApplyProfile(profilePath, historyDir, []byte(bodies[i]), stamps[i]); err != nil {
			t.Fatalf("ApplyProfile #%d returned error: %v", i, err)
		}
	}

	entries, err := ListHistory(historyDir)
	if err != nil {
		t.Fatalf("ListHistory returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	// 新しい順（11:00 が先、10:00 が後）。
	if !entries[0].Time.After(entries[1].Time) {
		t.Errorf("entries not newest-first: [0]=%v [1]=%v", entries[0].Time, entries[1].Time)
	}

	// HistoryContent が ID から中身を復元できる（最古の履歴 = v1）。
	body, err := HistoryContent(historyDir, entries[1].ID)
	if err != nil {
		t.Fatalf("HistoryContent returned error: %v", err)
	}
	if want := "search_status: searching\nrequired_skills:\n  - v1\n"; string(body) != want {
		t.Errorf("HistoryContent = %q, want %q", body, want)
	}
}

func TestListHistoryEmptyWhenNoDir(t *testing.T) {
	t.Parallel()

	entries, err := ListHistory(filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil {
		t.Fatalf("ListHistory returned error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("len(entries) = %d, want 0", len(entries))
	}
}

func TestUniqueHistoryPathAvoidsCollision(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.yaml")
	historyDir := filepath.Join(dir, "profile.history")

	writeProfile(t, profilePath, "search_status: searching\nrequired_skills:\n  - v1\n")

	// 同一秒の now で2回退避しても、2件とも残る（上書きしない）。
	now := time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)
	first, err := ApplyProfile(profilePath, historyDir, []byte("search_status: searching\nrequired_skills:\n  - v2\n"), now)
	if err != nil {
		t.Fatalf("first apply error: %v", err)
	}
	second, err := ApplyProfile(profilePath, historyDir, []byte(validProfileYAML), now)
	if err != nil {
		t.Fatalf("second apply error: %v", err)
	}
	if first.BackupPath == second.BackupPath {
		t.Fatalf("collision not avoided: both backups at %q", first.BackupPath)
	}

	entries, err := ListHistory(historyDir)
	if err != nil {
		t.Fatalf("ListHistory returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("len(entries) = %d, want 2 (no overwrite on same-second collision)", len(entries))
	}
}
