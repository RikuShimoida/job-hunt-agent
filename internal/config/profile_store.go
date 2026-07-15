package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	historyDirName         = "profile.history"
	historyFilePrefix      = "profile-"
	historyFileSuffix      = ".yaml"
	historyTimestampLayout = "20060102T150405Z"
)

// ProfileHistoryDir は profile.yaml のパスから履歴ディレクトリのパスを導く。
//
// --profile を別パスにしたときも履歴が同じディレクトリ配下へ追随するよう、
// 固定パスではなくプロフィールのディレクトリから導出する。
func ProfileHistoryDir(profilePath string) string {
	return filepath.Join(filepath.Dir(profilePath), historyDirName)
}

// ApplyResult は ApplyProfile の結果。
type ApplyResult struct {
	// BackupPath は退避先。現行 profile が無い初回は空。
	BackupPath string
}

// ApplyProfile は proposed を検証し、現行 profilePath を historyDir へ退避してから
// profilePath へ原子的に書き込む。now は退避ファイル名のタイムスタンプ源。
//
// 処理順は検証 → 退避 → 書き込み。検証に落ちた場合は履歴も profilePath も一切触らない
// （壊れた条件で上書きして次回実行が停止する事故と、失敗した提案で履歴が汚れるのを防ぐ）。
//
// 書き込みは proposed のバイト列をそのまま採用する（再マーシャルしない）。
// model へ unmarshal して marshal し直すと、記入例のコメントや項目順が毎回失われるため。
// 未正規化のスキル名があっても、実行時 LoadProfile がメモリ上で正規化するので採点は正しく動く。
func ApplyProfile(profilePath, historyDir string, proposed []byte, now time.Time) (ApplyResult, error) {
	if _, err := parseAndValidateProfile(proposed); err != nil {
		return ApplyResult{}, err
	}

	backupPath, err := backupCurrentProfile(profilePath, historyDir, now)
	if err != nil {
		return ApplyResult{}, err
	}

	if err := atomicWriteFile(profilePath, proposed); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{BackupPath: backupPath}, nil
}

// backupCurrentProfile は現行 profilePath を historyDir へタイムスタンプ付きで退避する。
// profilePath が存在しなければ（初回）退避せず空文字を返す。
func backupCurrentProfile(profilePath, historyDir string, now time.Time) (string, error) {
	current, err := os.ReadFile(profilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read current profile %s: %w", profilePath, err)
	}

	if err := os.MkdirAll(historyDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create history dir %s: %w", historyDir, err)
	}

	dst := uniqueHistoryPath(historyDir, now)
	if err := os.WriteFile(dst, current, 0o600); err != nil {
		return "", fmt.Errorf("failed to write history %s: %w", dst, err)
	}
	return dst, nil
}

// uniqueHistoryPath は now 由来の退避先パスを返す。同一秒での衝突時は連番で回避する。
func uniqueHistoryPath(historyDir string, now time.Time) string {
	ts := now.UTC().Format(historyTimestampLayout)
	base := historyFilePrefix + ts + historyFileSuffix
	path := filepath.Join(historyDir, base)
	for i := 2; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
		path = filepath.Join(historyDir, fmt.Sprintf("%s%s-%d%s", historyFilePrefix, ts, i, historyFileSuffix))
	}
}

// atomicWriteFile は同一ディレクトリの一時ファイルへ書いてから rename する。
// 途中失敗で dst が破損しないようにする（同一 FS 上の rename は原子的）。
func atomicWriteFile(dst string, body []byte) error {
	dir := filepath.Dir(dst)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create dir %s: %w", dir, err)
		}
	}

	tmp, err := os.CreateTemp(dir, ".profile-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) //nolint:errcheck // rename 成功後は既に無く、失敗時の残骸掃除は best-effort

	if _, err := tmp.Write(body); err != nil {
		tmp.Close() //nolint:errcheck // 書き込み失敗が本来のエラー。close の失敗は覆い隠さない
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close() //nolint:errcheck // 同上
		return fmt.Errorf("failed to chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("failed to rename temp file to %s: %w", dst, err)
	}
	return nil
}

// HistoryEntry は退避済みの過去条件1件。
type HistoryEntry struct {
	// ID は --show で指定する識別子（ファイル名からタイムスタンプ部分を取り出したもの）。
	ID   string
	Path string
	Time time.Time
}

// ListHistory は historyDir 配下の退避ファイルを新しい順に返す。
// ディレクトリが無ければ空スライスを返す（エラーにしない）。
func ListHistory(historyDir string) ([]HistoryEntry, error) {
	entries, err := os.ReadDir(historyDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read history dir %s: %w", historyDir, err)
	}

	out := make([]HistoryEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, historyFilePrefix) || !strings.HasSuffix(name, historyFileSuffix) {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(name, historyFilePrefix), historyFileSuffix)
		// タイムスタンプの解釈に失敗しても Path は返せるため、Time はゼロ値のまま拾う。
		t, _ := time.Parse(historyTimestampLayout, id)
		out = append(out, HistoryEntry{ID: id, Path: filepath.Join(historyDir, name), Time: t})
	}

	// ファイル名は固定幅のタイムスタンプ書式であり、辞書順の降順が新しい順に一致する。
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// HistoryContent は id に対応する退避ファイルの中身を返す。
func HistoryContent(historyDir, id string) ([]byte, error) {
	path := filepath.Join(historyDir, historyFilePrefix+id+historyFileSuffix)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read history %s: %w", path, err)
	}
	return b, nil
}
