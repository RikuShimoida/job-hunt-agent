package fixture_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/RikuShimoida/job-hunt-agent/internal/connector/fixture"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("failed to write fixture %s: %v", name, err)
	}
}

func TestFetchEmail(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "002.txt", "二番目")
	writeFile(t, dir, "001.txt", "一番目")
	writeFile(t, dir, "ignore.md", "対象外の拡張子")

	c := fixture.NewEmail("fixture-email", dir)

	raws, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() returned error: %v", err)
	}

	if len(raws) != 2 {
		t.Fatalf("取得件数 = %d, want 2（.txt のみ）", len(raws))
	}
	// ファイル名の昇順で読むこと（実行ごとに順序が変わらない）。
	if raws[0].ExternalID != "001.txt" || raws[1].ExternalID != "002.txt" {
		t.Errorf("読み込み順 = [%s %s], want [001.txt 002.txt]",
			raws[0].ExternalID, raws[1].ExternalID)
	}
	if raws[0].Body != "一番目" {
		t.Errorf("Body = %q, want 一番目", raws[0].Body)
	}
	if raws[0].Format != "email" {
		t.Errorf("Format = %q, want email", raws[0].Format)
	}
	if raws[0].SourceName != "fixture-email" {
		t.Errorf("SourceName = %q, want fixture-email", raws[0].SourceName)
	}
}

func TestFetchHTML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "001.html", "<h1>案件</h1>")
	writeFile(t, dir, "note.txt", "対象外の拡張子")

	c := fixture.NewHTML("fixture-html", dir)

	raws, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() returned error: %v", err)
	}

	if len(raws) != 1 {
		t.Fatalf("取得件数 = %d, want 1（.html のみ）", len(raws))
	}
	if raws[0].Format != "html" {
		t.Errorf("Format = %q, want html", raws[0].Format)
	}
}

func TestFetchEmptyDirReturnsNoJobs(t *testing.T) {
	t.Parallel()

	c := fixture.NewEmail("fixture-email", t.TempDir())

	raws, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() returned error: %v", err)
	}
	if len(raws) != 0 {
		t.Errorf("取得件数 = %d, want 0", len(raws))
	}
}

func TestFetchMissingDirReturnsError(t *testing.T) {
	t.Parallel()

	c := fixture.NewEmail("fixture-email", filepath.Join(t.TempDir(), "not-exist"))

	// ディレクトリが無いのに 0件成功で返すと「取れなかった」ことに気づけない。
	if _, err := c.Fetch(context.Background()); err == nil {
		t.Fatal("存在しないディレクトリでエラーが返らなかった")
	}
}

func TestFetchRespectsCanceledContext(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "001.txt", "本文")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := fixture.NewEmail("fixture-email", dir)

	if _, err := c.Fetch(ctx); err == nil {
		t.Fatal("キャンセル済み context でエラーが返らなかった")
	}
}
