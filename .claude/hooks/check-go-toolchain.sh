#!/bin/bash
# /impl スキル実行前に Go ツールチェーンが揃っているか確認する PreToolUse Hook
#
# 「/impl のときだけ」走らせるゲートを二重に持つ:
#   1. settings.json の PreToolUse[matcher=Skill] の `if: "Skill(impl *)"`
#   2. 本スクリプトが stdin の tool_input.skill を読んで自前判定する（下の SKILL 判定）
#
# なぜ (1) だけに頼らず (2) を持つか（Issue #5 の検証結果, 2026-07-16）:
#   `Skill(...)` 形式の `if` が実際に評価されるかを実挙動で検証した。本スクリプトへ一時プローブを
#   仕込み、非 impl スキルを Skill ツールで呼んだ結果——`if` あり: フック非発火 / `if` を外す: 発火
#   ——を観測し、`if` は正しく評価されて impl 以外を除外すると確認した（settings.json はセッション中に
#   ホットリロードされた）。それでも (2) を残すのは、settings.json を手編集して `if` を落としても
#   /impl 以外のスキルを巻き込んでブロックしない安全網とするため。他フック（bypass-heuristic.sh 等）が
#   jq で tool_input を読んで自前ゲートしている流儀にも揃う。settings.json は厳密 JSON でコメントを
#   持てないため、検証結果はゲートと同じ本ファイルに記録する。
#
# 再現手順（第三者が挙動を確かめるとき / AC#2 の検証）:
#   echo '{"tool_input":{"skill":"plan"}}' | bash check-go-toolchain.sh; echo $?   # => 0（impl 以外は素通し）
#   echo '{"tool_input":{"skill":"impl"}}' | bash check-go-toolchain.sh; echo $?   # => 0（go/golangci-lint があれば）/ 2（欠けていれば）

SKILL=$(jq -r '.tool_input.skill // empty' 2>/dev/null)
if [ "$SKILL" != "impl" ]; then
  exit 0
fi

REPO_ROOT="${CLAUDE_PROJECT_DIR:-.}"

# go.mod が無い（= まだ Go プロジェクトとして初期化されていない）場合は通す。
# ブートストラップ作業そのものを /impl で行えるようにするため。
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
