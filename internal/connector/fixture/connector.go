// Package fixture はローカルの testdata から案件を読むコネクタ。
//
// Phase 1 では外部サービスへ接続しないため、これが唯一のコネクタになる。
// Gmail（Phase 3）・公開 Web（Phase 4）も同じ port.Connector を実装して差し替える。
package fixture

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

// Format は fixture の中身の形式。
type Format string

const (
	FormatEmail Format = "email"
	FormatHTML  Format = "html"
)

// Connector は指定ディレクトリ配下のファイルを1件1案件として読む。
type Connector struct {
	name   string
	dir    string
	format Format
	ext    string
}

// NewEmail は testdata/emails のような *.txt を読むコネクタを返す。
func NewEmail(name, dir string) *Connector {
	return &Connector{name: name, dir: dir, format: FormatEmail, ext: ".txt"}
}

// NewHTML は testdata/html のような *.html を読むコネクタを返す。
func NewHTML(name, dir string) *Connector {
	return &Connector{name: name, dir: dir, format: FormatHTML, ext: ".html"}
}

func (c *Connector) Name() string { return c.name }

// Fetch はディレクトリ配下のファイルをファイル名の昇順で読む。
// ディレクトリが存在しない場合はエラーを返す（0件で黙って成功すると
// 「取れなかった」ことに気づけないため）。
func (c *Connector) Fetch(ctx context.Context) ([]model.RawJob, error) {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read fixture dir %s: %w", c.dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), c.ext) {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	jobs := make([]model.RawJob, 0, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("fixture fetch canceled: %w", err)
		}
		path := filepath.Join(c.dir, name)
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read fixture %s: %w", path, err)
		}
		jobs = append(jobs, model.RawJob{
			SourceName: c.name,
			Body:       string(body),
			Format:     string(c.format),
			ExternalID: name,
		})
	}
	return jobs, nil
}
