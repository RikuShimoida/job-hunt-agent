# job-hunt-agent

複数のエージェント・メール・Web サイトに分散した案件情報を自動で収集し、
希望条件との一致度を採点して、重複を除いた有望案件だけを通知する CLI ツール（Go）。

> **現状: Phase 0 〜 Phase 3 完了。**
> **Gmail の実案件が Slack へ届く。** 公開 Web スクレイピングへは**まだ接続していない**。
> 仕様・設計判断は [docs/architecture.md](docs/architecture.md) に集約している。

## できること

```
Gmail（案件メール）＋ testdata（メール / HTML）
      ↓ collect    案件を収集し、正規化して SQLite へ保存（重複は登録せず、内容の変更は反映）
      ↓ score      プロフィールと照合し、除外判定と 0〜100 点の採点
      ↓ notify     閾値以上かつ未通知の案件を、推奨理由・懸念・応募リンクつきで Slack へ送信
```

出力例:

```
【95点・新着】Java／AWS 基盤改善案件
単価：800000円　稼働：週4日　開始：不明
勤務：フルリモート　紹介元：gmail-agents
主要スキル：Java、Spring、AWS
推奨理由：
・希望単価 800000円 に到達している
・フルリモートで出社が不要
・得意スキルの Spring、Docker、Terraform が一致する
・週4日で稼働でき、希望する稼働日数に合う
懸念：
・開始時期が案件情報に記載されていない
応募：https://share.hsforms.test/abc123
```

通知は「なぜこの案件が自分に合うのか（**推奨理由**）」「何に注意すべきか（**懸念**）」を
箇条書きで示し、そのまま応募できるリンクを載せる。該当が0件の項目は行ごと出さない。

- **推奨理由** — 加点された理由を自然文で並べる
- **懸念** — 希望と合わない点に加え、**案件情報から読み取れなかった項目**
  （単価 / 開始時期 / リモート / 稼働日数）を挙げる
- **応募** — 応募フォームの URL（クラウドテック）。**URL** は案件詳細ページ（フォスターネット）。
  ソースによってどちらか一方しか持たないため、両方を別々に出す

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

- `config/profile.yaml` — 求職状態・単価・稼働・リモート・スキル・役割・除外条件・通知閾値・応募返信の素材（`application`）
- `config/sources.yaml` — 各ソースの有効／無効・種別・パス
- `.env` — 下表の環境変数

| 変数 | 必須 | 用途 |
|---|---|---|
| `DATABASE_URL` | 任意（既定 `./job-hunt-agent.db`） | SQLite のファイルパス |
| `LOG_LEVEL` | 任意（既定 `info`） | `debug` / `info` / `warn` / `error` |
| `SLACK_WEBHOOK_URL` | **実送信時は必須** | 案件通知の送信先（Incoming Webhook） |
| `SLACK_ERROR_WEBHOOK_URL` | 任意 | ソース取得失敗の送信先。未設定ならエラーは Slack へ送らず構造化ログにのみ残す |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` / `GOOGLE_REFRESH_TOKEN` | **Gmail 利用時は必須** | Gmail の読み取り。1つでも欠けると起動時に停止する |

`--dry-run` なしで `SLACK_WEBHOOK_URL` が未設定なら**起動時に停止する**。
黙って標準出力へフォールバックすると「送ったつもりで送られていない」事故になるため。

### Gmail を繋ぐ

```bash
# 1. Google Cloud Console でプロジェクトを作り、Gmail API を有効化する
# 2. OAuth クライアント ID（種類: デスクトップアプリ）を発行し、.env に設定する
#      GOOGLE_CLIENT_ID / GOOGLE_CLIENT_SECRET
# 3. リフレッシュトークンを取得して .env に貼る（表示された URL をブラウザで開いて認可する）
go run ./cmd/job-hunt-agent auth gmail

# 4. config/sources.yaml の gmail ソースを enabled: true にする
```

要求するスコープは **`gmail.readonly` のみ**。削除・返信・ラベル変更は一切行わない。

### 応募返信メールの下書き

応募に返信を要する案件向けに、`profile.yaml` の `application` セクションへ自己紹介・経歴サマリ・
強み・志望動機の定型文を書いておける。これを素材に、**claude.ai の Gmail コネクタ経由で
返信文の「下書き」を作る**（送信は本人が行う。下書きまで）。

**この下書き作成に job-hunt-agent（Go）は関与しない。** Go ツールは `gmail.readonly` のまま
Gmail へ一切書き込まず、`application` セクションは採点にも使わない（下書きの素材として読むだけ）。
全項目が任意で、未記入でも `profile validate` は通る。

対応しているエージェントは**クラウドテック**と**フォスターネット**の2社。この2社だけが
採点に必要な単価・稼働・リモート・スキルをメール本文に持つ。取得は**送信元アドレスで絞る**
（キーワード検索にすると転職サイトの求人メールに埋もれるため）。

Remogu（本文に単価もスキルも無い）・フリーランスハブ（1メールに複数案件）・
ギークス（案件データなし）は対象外。理由は [docs/architecture.md](docs/architecture.md) を参照。

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

### 希望条件をインタビューで更新する

`config/profile.yaml` はテキスト編集もできるが、`/set-conditions` スキルを使うと**現在値を見せながら
1項目ずつ**確認して更新できる（項目名・書式・`remote_required` と `max_onsite_days` の同時指定不可
といった制約を覚えなくてよい）。差分を確認して承認すると保存される。

保存の実体は `profile apply` で、**現行 `profile.yaml` を履歴へ退避してから**上書きする。
検証に落ちた提案は保存されず、現行ファイルは一切変更されない。書き込みは一時ファイル経由の
rename で原子的に行うため、中断しても `profile.yaml` は壊れない。
apply は `profile validate` と同じ検証に加え、**未知のキー（`remote_requird` のようなタイポ）も弾く**
（黙って無視して no-op 保存になるのを防ぐ）。

```bash
# スキルを使わず、組み立て済みの YAML を直接適用することもできる
job-hunt-agent profile apply --from /path/to/new-profile.yaml

# 過去に探していた条件を一覧・表示する（戻したいときの手がかり）
job-hunt-agent profile history
job-hunt-agent profile history --show 20260715T141558Z
```

履歴は `config/profile.history/` に退避される。個人の希望条件を含むため、`profile.yaml` と同様に
**Git 管理外**（`.gitignore` 済み）。更新すれば次回の `collect` / `score` / `run` から新条件で動く
（パイプラインは毎回 `profile.yaml` を読み直す。仕組みは変わらない）。

## コマンド

| コマンド | 説明 |
|---|---|
| `job-hunt-agent init` | 設定ファイルと `.env` を生成する（既存は上書きしない） |
| `job-hunt-agent auth gmail` | Gmail の読み取り専用トークンを取得する |
| `job-hunt-agent profile validate` | プロフィール設定を検証する |
| `job-hunt-agent profile apply --from <file>` | 提案された profile を検証し、現行を履歴退避してから保存する |
| `job-hunt-agent profile history [--show <id>]` | 退避済みの過去条件を一覧・表示する |
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
- 推奨理由の**文言だけ**が変わっても再通知しない（応募 URL の変化も再通知の理由にならない）
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

Phase 4 以降で実装する。

- **公開 Web コネクタ**（Phase 4。実装前に公開取得の可否と利用条件を確認する）
- **Remogu**（メール本文に単価もスキルも無く、案件ページの取得が要るため Phase 4 の領分）
- **フリーランスハブ**（1メールに複数案件。「1メール = 1案件」のモデル前提を変える必要がある）
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
| `/set-conditions` | 希望条件（`profile.yaml`）をインタビュー形式で更新する（現在値を見せ、差分確認後に保存） |
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
