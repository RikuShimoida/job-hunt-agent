#!/bin/bash
# PostToolUse hook: Goソース変更後に gofmt + golangci-lint --fix を自動実行
#
# 入力は stdin の JSON（{"tool_name":..., "tool_input":{"file_path":...}}）。
# 環境変数 TOOL_INPUT は存在しないため参照してはならない（参照すると無言で no-op になる）。
#
# ツール未導入の環境（Go 未インストール等）では黙ってスキップする。
# ここで exit 2 を返すと編集のたびにブロックされ、ブートストラップ前のリポジトリで作業できなくなるため。

FILE_PATH=$(jq -r '.tool_input.file_path // empty')

case "$FILE_PATH" in
  *.go) ;;
  *) exit 0 ;;
esac

command -v gofmt >/dev/null 2>&1 || exit 0

gofmt -w "$FILE_PATH" 2>&1

if command -v golangci-lint >/dev/null 2>&1; then
  cd "${CLAUDE_PROJECT_DIR:-.}" || exit 0
  golangci-lint run --fix "$(dirname "$FILE_PATH")/..." 2>&1 || true
fi

exit 0
