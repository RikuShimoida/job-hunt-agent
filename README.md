# job-hunt-agent

複数のエージェント・メール・Web サイトに分散した案件情報を自動で収集し、
希望条件との一致度を採点して、重複を除いた有望案件だけを通知する CLI ツール（Go）。

> **現状: Phase 0 / Phase 1 / Phase 2 完了。**
> **Slack へは実際に通知が届く。** Gmail / Web スクレイピングへは**まだ接続していない**。
> `testdata` の架空サンプルから収集 → 正規化 → SQLite 保存 → 採点 → Slack 通知までが動く。
> 仕様・設計判断は [docs/architecture.md](docs/architecture.md) に集約している。

## できること

```
testdata（メール / HTML）
      ↓ collect    案件を収集し、正規化して SQLite へ保存（重複は登録せず、内容の変更は反映）
      ↓ score      プロフィールと照合し、除外判定と 0〜100 点の採点
      ↓ notify     閾値以上かつ未通知の案件を、加点・減点理由つきで Slack へ送信
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
- `.env` — 下表の環境変数

| 変数 | 必須 | 用途 |
|---|---|---|
| `DATABASE_URL` | 任意（既定 `./job-hunt-agent.db`） | SQLite のファイルパス |
| `LOG_LEVEL` | 任意（既定 `info`） | `debug` / `info` / `warn` / `error` |
| `SLACK_WEBHOOK_URL` | **実送信時は必須** | 案件通知の送信先（Incoming Webhook） |
| `SLACK_ERROR_WEBHOOK_URL` | 任意 | ソース取得失敗の送信先。未設定ならエラーは Slack へ送らず構造化ログにのみ残す |

`--dry-run` なしで `SLACK_WEBHOOK_URL` が未設定なら**起動時に停止する**。
黙って標準出力へフォールバックすると「送ったつもりで送られていない」事故になるため。

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
| `job-hunt-agent notify [--dry-run]` | 閾値以上かつ未通知の案件を Slack へ通知する |
| `job-hunt-agent run [--dry-run]` | collect → score → notify を順に実行する |

`--dry-run` を付けると Slack へ送らず、送信予定の内容を標準出力へ表示する。

### 通知済み管理

同じ案件が何度も届くと通知はノイズになり、やがて見なくなる。そのため:

- 一度送信に成功した案件は**再通知しない**（`run` を何度実行しても届かない）
- ただし**重要な変更**（単価・リモート頻度・開始時期・必須スキル）があり、
  再評価後も閾値以上なら `【95点・更新】` として再通知する
- 加点理由の**文言だけ**が変わっても再通知しない
- 有望案件が0件なら Slack へ何も送らない（無音）
- 送信に失敗した案件は通知済みにせず、次回実行で再送する
- 中断（Ctrl-C / SIGTERM）しても、**Slack へ届いた案件は必ず通知済みとして記録する**
  （記録が残らないと次回また届いてしまうため）

「更新」通知には**何がどう変わったか**を見出しの直下に表示する。

```
【95点・更新】Java／AWS 基盤改善案件
変更：
・単価：750000〜850000円 → 900000〜1000000円
・リモート：フルリモート → ハイブリッド（週2日出社）
単価：900000〜1000000円　稼働：週3日　開始：2026-09-01
勤務：ハイブリッド（週2日出社）　紹介元：fixture-email
…
```

変わっていない項目は差分に出さない。この機能を入れる前に通知済みだった案件は
前回のスナップショットを持たないため、初回の「更新」通知だけ差分行が出ない
（見出しが `【95点・更新】` になるだけ）。

`--dry-run` は**通知済みとして記録しない**（実際には送っていないため）。

`collect` / `score` は案件通知を行わないため `SLACK_WEBHOOK_URL` を要求しない。
ソース取得失敗を Slack のエラーチャンネルへ送るのは `run`（`--dry-run` なし）。
`collect` 単体は手元での確認用。定期実行には `run` を使う。

### 送信レートと終了コード

- Slack の 429 を避けるため、案件は**1件ごとに1秒空けて**送る
- **1件でも送信に失敗すると `notify` / `run` は非ゼロ終了する**
  （定期実行で失敗が赤くならないと、通知が届いていないことに気づけないため）。
  案件の保存・採点は完了しており、失敗した案件は次回実行で再送される

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
- **通知**: Slack Incoming Webhook（標準 `net/http`。`slack-go/slack` は使わない）
- **ログ**: 標準 `log/slog`（JSON 構造化ログ）
- **テスト**: 標準 `testing`（テーブル駆動）。testify は使わない
- **Lint**: golangci-lint v2 / **CI**: GitHub Actions

## 現時点の対象外

Phase 3 以降で実装する。

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
