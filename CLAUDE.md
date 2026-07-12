## 参照ルール

実装時は必ず @README.md @docs/architecture.md を参照すること。
設計ドキュメントが未整備の領域は「未定義」として扱い、推測で埋めない（ユーザーに確認する）。

## コーディングルール

- コメントは Why not（なぜ別のやり方を採用しなかったか）のみ。What/How/Why 禁止
- デバッグコードは最終コードに残さない
- Go 固有の規約は @.claude/rules/go.md を参照

## 作業ルール

- 推測実装・「それっぽく動く」実装は禁止。仕様不明時はユーザーに確認
- 実装は Issue 起点。`/plan` で計画 → 承認 → `/impl` で実装 → `/pr-review` → `/merge`
- 1タスク = 1 worktree。develop メインの作業ツリーは司令塔として常に clean に保つ（@docs/worktree-workflow.md）

## 標準コマンド（唯一の正）

スキル・フック・エージェントはすべてこのコマンド定義に従う。

| 目的 | コマンド |
|---|---|
| フォーマット | `gofmt -w .` |
| 静的解析 | `go vet ./...` |
| Lint | `golangci-lint run`（自動修正は `golangci-lint run --fix`） |
| ユニットテスト | `go test ./...` |
| レース検出 | `go test -race ./...` |
| 統合テスト | `go test -tags=integration ./...` |
| ビルド | `go build ./...` |
| 依存整理 | `go mod tidy` |

品質チェックの順序は **format → vet → lint → test**（順序厳守）。

## ブランチ運用

- ベースブランチは `develop`。`main` は本番リリース用
- feature ブランチ: `feature/<issue番号>-<英語スラッグ>` / `bugfix/<issue番号>-<英語スラッグ>`
- PR のベースは常に `develop`
- **`develop` は非デフォルトブランチのため、PR 本文の `Closes #N` では Issue が自動クローズされない**。
  Issue のクローズは `/merge` スキルが `gh issue close` で明示的に行う

## 禁止事項（違反厳禁）

- `run_in_background: true` の使用
- 推測実装（仕様不明のまま「それっぽく動く」コードを書くこと）
- テスト未実行でのコミット
- 破壊的 Git コマンド（`git push --force` / `git reset --hard` / `git rebase` / `git branch -D`）のユーザー確認なしでの実行
