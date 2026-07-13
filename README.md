# job-hunt-agent

複数のエージェント・メール・Web サイトに分散した案件情報を自動で収集し、
希望条件との一致度を採点して、重複を除いた有望案件だけを通知する CLI ツール（Go）。

> **現状: Phase 0 / Phase 1 完了。**
> 外部サービス（Gmail / Web スクレイピング / Slack）へは**まだ接続していない**。
> `testdata` の架空サンプルから収集 → 正規化 → SQLite 保存 → 採点 → dry-run 通知までが動く。
> 仕様・設計判断は [docs/architecture.md](docs/architecture.md) に集約している。

## できること

```
testdata（メール / HTML）
      ↓ collect    案件を収集し、正規化して SQLite へ保存（重複は登録しない）
      ↓ score      プロフィールと照合し、除外判定と 0〜100 点の採点
      ↓ notify     閾値以上の案件だけを、加点・減点理由つきで出力
```

出力例:

```
【95点・新着】Java／AWS 基盤改善案件
単価：750000〜850000円　稼働：週3日　開始：2026-09-01
勤務：フルリモート　紹介元：fixture-email、fixture-html
勤務地：東京
主要スキル：Java、Spring、AWS
加点：希望単価以上（750000〜850000円）、フルリモート、得意スキル4件一致（Spring、Docker、Terraform、TypeScript）、…
URL：https://example-agent.test/jobs/001
```

## セットアップ

```bash
# Go ツールチェーン（1.26 以降）
brew install go

# Lint
brew install golangci-lint

# 設定ファイルと .env を生成する（既存ファイルは上書きしない）
go run ./cmd/job-hunt-agent init

# 動かしてみる
make run-dry
```

`init` は `config/profile.example.yaml` / `config/sources.example.yaml` / `.env.example` から
`config/profile.yaml` / `config/sources.yaml` / `.env` を生成する。
**実値を入れた設定ファイルと `.env` は Git 管理しない**（`.gitignore` 済み）。

### 設定

- `config/profile.yaml` — 求職状態・単価・稼働・リモート・スキル・役割・除外条件・通知閾値
- `config/sources.yaml` — 各ソースの有効／無効・種別・パス
- `.env` — `DATABASE_URL`（既定 `./job-hunt-agent.db`）、`LOG_LEVEL`（既定 `info`）

設定のうち、解釈を間違えやすい2つ。

- **`remote_required: true` は「出社0日のみ許容」**。常駐だけでなく、
  ハイブリッド（週N日出社）の案件も除外する。出社を許容したい場合は
  `remote_required: false` + `max_onsite_days: N` を使う（両者は同時指定できない）。
- **`excluded_keywords` は案件名と概要だけを照合する**。メール原文までは見ない
  （「常駐必須ではありません」のような否定文や署名・引用で誤除外されるため）。

`run` / `collect` は Ctrl-C（SIGINT）と SIGTERM で中断できる。

求職状態は3つ。

| `search_status` | 挙動 |
|---|---|
| `searching` | 60点以上を通知 |
| `watching` | 80点以上のみ通知（探していない時期も良案件は見逃さない） |
| `paused` | 収集も通知も行わない |

## コマンド

| コマンド | 説明 |
|---|---|
| `job-hunt-agent init` | 設定ファイルと `.env` を生成する（既存は上書きしない） |
| `job-hunt-agent profile validate` | プロフィール設定を検証する |
| `job-hunt-agent collect [--source <name>]` | 案件を収集して保存する |
| `job-hunt-agent score` | 保存済み案件を再評価する |
| `job-hunt-agent notify --dry-run` | 閾値以上の案件を標準出力へ表示する |
| `job-hunt-agent run --dry-run` | collect → score → notify を順に実行する |

Phase 1 では Slack 送信が未実装のため、`notify` / `run` は `--dry-run` が必須。

## 標準コマンド

| 目的 | コマンド | make |
|---|---|---|
| フォーマット | `gofmt -w .` | `make fmt` |
| 静的解析 | `go vet ./...` | `make vet` |
| Lint | `golangci-lint run` | `make lint` |
| ユニットテスト | `go test ./...` | `make test` |
| レース検出 | `go test -race ./...` | `make test-race` |
| 統合テスト | `go test -tags=integration ./...` | `make test-integration` |
| ビルド | `go build ./...` | `make build` |
| dry-run 実行 | — | `make run-dry` |

品質チェックの順序は **format → vet → lint → test**（`make check`）。

## 技術スタック

- **言語**: Go 1.26（`go.mod` の `go` ディレクティブが唯一の正）
- **CLI**: Cobra / **設定**: YAML + 環境変数
- **永続化**: SQLite（`modernc.org/sqlite`。CGO 不要）
- **HTML 解析**: `golang.org/x/net/html`
- **ログ**: 標準 `log/slog`（JSON 構造化ログ）
- **テスト**: 標準 `testing`（テーブル駆動）。testify は使わない
- **Lint**: golangci-lint v2 / **CI**: GitHub Actions

## 現時点の対象外

Phase 2 以降で実装する。

- **Slack への実送信**と通知済み管理（Phase 2）
- **Gmail** 読み取り専用 OAuth（Phase 3）
- **公開 Web コネクタ**（Phase 4。実装前に公開取得の可否と利用条件を確認する）
- 類似度ベースの重複排除・リトライ・構造変更検知・`status` サブコマンド（Phase 5）

プロダクトとして**やらないこと**（自動応募・自動返信・スキルシート自動送信・Gmail の変更操作・
ログイン突破 / CAPTCHA 回避・利用規約で禁止されたスクレイピング・マルチユーザー・
生成 AI API の必須化）は [docs/architecture.md](docs/architecture.md) を参照。

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
- [docs/architecture.md](docs/architecture.md) — 構成・設計判断・スコアリング仕様
- [docs/worktree-workflow.md](docs/worktree-workflow.md) — worktree 運用の設計
- `.claude/rules/` — コーディング規約・テスト規約・コマンド実行ルール
