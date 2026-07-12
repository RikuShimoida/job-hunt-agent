# job-hunt-agent

就職活動を支援するエージェントプロダクト（Go）。

> **現状**: 開発ワークフローの整備が完了した段階。プロダクト本体はこれから。
> 仕様・構成は `docs/architecture.md` に集約していく（現時点では大半が未定義）。

## 技術スタック

- **言語**: Go
- **Lint**: golangci-lint
- **テスト**: 標準 `testing`（テーブル駆動）
- **ホスティング**: GitHub

## 標準コマンド

| 目的 | コマンド |
|---|---|
| フォーマット | `gofmt -w .` |
| 静的解析 | `go vet ./...` |
| Lint | `golangci-lint run` |
| ユニットテスト | `go test ./...` |
| レース検出 | `go test -race ./...` |
| 統合テスト | `go test -tags=integration ./...` |
| ビルド | `go build ./...` |

品質チェックの順序は **format → vet → lint → test**。

## 開発フロー

Issue 駆動。1タスク = 1 worktree。`develop` メインの作業ツリーは司令塔として常に clean に保つ。

```
/create-issue  → 課題メモ / Evernote から GitHub Issue を起票
      ↓
/plan <N>      → Issue から実装計画を作成（ユーザー承認まで実装しない）
      ↓
/impl <N>      → worktree を切って実装 → テスト → 品質チェック → PR作成
      ↓
/pr-review <N> → system-architect がレビューし PR にコメント（impl から自動起動）
      ↓
/review-fix <N>（指摘を取り込む場合） または /merge <N>（マージする場合）
```

`/merge` は CI の完了を見届け、全チェック成功時のみ Merge commit でマージし、
リモート・ローカルのブランチ削除と Issue のクローズまで行う。

### その他のスキル

| スキル | 用途 |
|---|---|
| `/ask` | 仕様・設計・ドメインに関する質問（推測せず、根拠を示して回答） |
| `/clarify` | 大規模機能の要件深掘り |
| `/report` | 作業完了時の品質チェックと報告 |
| `/sync-docs` | 実装内容を設計ドキュメントへ反映 |
| `/understand` | 実装理解チェック（Spring Boot 対比で Go の概念を翻訳） |
| `/dev-doctor` | 開発環境トラブルのトリアージ |
| `/build-problem` | ビルド・モジュール依存の診断 |
| `/git-problem` | Git パフォーマンス問題の診断 |

## ブランチ運用

- ベースブランチは `develop`。`main` は本番リリース用
- feature ブランチ: `feature/<issue番号>-<英語スラッグ>` / `bugfix/<issue番号>-<英語スラッグ>`
- PR のベースは常に `develop`
- **`develop` は非デフォルトブランチのため `Closes #N` による Issue 自動クローズが効かない**。
  クローズは `/merge` が明示的に行う

## ドキュメント

- [CLAUDE.md](CLAUDE.md) — プロジェクト全体のルールと標準コマンド
- [docs/architecture.md](docs/architecture.md) — 構成・設計判断
- [docs/worktree-workflow.md](docs/worktree-workflow.md) — worktree 運用の設計
- `.claude/rules/` — コーディング規約・テスト規約・コマンド実行ルール

## セットアップ

```bash
# Go ツールチェーン
brew install go

# Lint
brew install golangci-lint
```
