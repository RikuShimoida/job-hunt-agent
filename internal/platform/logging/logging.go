// Package logging は構造化ログを組み立てる。
package logging

import (
	"io"
	"log/slog"
	"strings"
)

// New は LOG_LEVEL に応じた構造化ロガーを返す。
// 未知のレベル文字列は info として扱う（ログ設定の誤りで起動を止めないため）。
func New(w io.Writer, level string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: parseLevel(level),
	}))
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
