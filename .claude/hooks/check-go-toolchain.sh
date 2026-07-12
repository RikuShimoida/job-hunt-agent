#!/bin/bash
# implスキル実行前に Go ツールチェーンが揃っているか確認する
#
# go.mod が無い（= まだ Go プロジェクトとして初期化されていない）場合は通す。
# ブートストラップ作業そのものを /impl で行えるようにするため。

REPO_ROOT="/Users/rikushimoida/Documents/repository/job-hunt-agent"

if [ ! -f "$REPO_ROOT/go.mod" ]; then
  exit 0
fi

if ! command -v go >/dev/null 2>&1; then
  echo 'BLOCKED: go コマンドが見つかりません。Go ツールチェーンをインストールしてから実装タスクを開始してください。' >&2
  exit 2
fi

if ! command -v golangci-lint >/dev/null 2>&1; then
  echo 'BLOCKED: golangci-lint が見つかりません。品質チェックに必要です（brew install golangci-lint）。' >&2
  exit 2
fi

exit 0
