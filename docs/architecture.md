# アーキテクチャ

job-hunt-agent の構成・設計判断を集約するドキュメント。
`/sync-docs` スキルの同期対象であり、**コードを読まなくてもここを見れば仕様が分かる状態**を保つ。

> **現状: Phase 0 / Phase 1 / Phase 2 完了。** Slack への実送信と通知済み管理まで動く。
> Phase 3 以降（Gmail / 公開 Web）は未実装。未実装の領域は「未定義」と明記し、
> **推測で埋めない**。

---

## 1. プロダクト概要

複数のエージェント・メール・Web サイトに分散した案件情報を自動で収集し、
希望条件との一致度を採点して、重複を除いた有望案件だけを通知するツール。

案件探しそのものではなく、応募判断・面談準備・スキルアップへ時間を使えるようにすることが目的。

### 解決する課題

- 案件情報が多数のエージェント・メール・Web サイトへ分散しており、巡回だけで時間がかかる
- 同じ案件が別の紹介元から届き、重複確認が必要になる
- 仕事を探していない時期でも良案件を見逃したくない
- 単価・稼働・リモート・開始時期・スキルを目視比較する負担が大きい

### スコープ

**やること**: 案件の収集・正規化・重複排除・除外判定・スコアリング・通知。

**やらないこと**（明示的に対象外）:

- 案件への自動応募・自動返信・自動面談予約
- スキルシートの自動送信
- Gmail の削除・アーカイブ・既読化・ラベル変更（読み取り専用スコープのみ）
- ログイン突破・CAPTCHA 回避・二要素認証の自動化
- 利用規約で禁止されているスクレイピング
- 生成 AI API を必須にすること（まずルールベース。必要なら後から抽出補助として追加）
- マルチユーザー・課金・権限管理（利用者は本人1名のみ）

### 提供形態

CLI（単一バイナリ）。定期実行は GitHub Actions の schedule または常駐サーバーを想定。

### 実装フェーズ

| Phase | 内容 | 状態 |
|---|---|---|
| 0 | リポジトリ初期化（Go Modules / Cobra / lint / CI / Makefile） | **完了** |
| 1 | 外部サービスなしの縦切り MVP（fixture → 正規化 → SQLite → 採点 → dry-run 通知） | **完了** |
| 2 | Slack Incoming Webhook 接続と通知済み管理 | **完了** |
| 3 | Gmail 読み取り専用 OAuth と案件メール解析（クラウドテック / フォスターネット） | **完了** |
| 4 | 公開 Web コネクタ（実装前に公開取得の可否と利用条件を確認する） | 未着手 |
| 5 | 類似度ベースの重複排除・リトライ・構造変更検知・実行履歴強化・`status` サブコマンド | 未着手 |
| 6 | 案件ソースの追加、抽出精度とスコア重みの実データ改善 | 未着手 |

## 2. 技術構成

| 項目 | 選択 | 備考 |
|---|---|---|
| 言語 | Go | 確定 |
| Go バージョン | 1.26.5 | `go.mod` の `go` ディレクティブが唯一の正。CI は `go-version-file: go.mod` で解決する |
| CLI | `spf13/cobra` | 確定 |
| 設定 | YAML（`gopkg.in/yaml.v3`）+ 環境変数 | 実値は Git 管理しない |
| 永続化 | SQLite（`database/sql` + `modernc.org/sqlite`） | CGO 不要。将来必要なら PostgreSQL へ移行 |
| マイグレーション | 自前の簡易ランナー（`migrations/*.sql`） | `golang-migrate` は却下（§7） |
| HTML 解析 | `golang.org/x/net/html` | `goquery` は却下（§7） |
| 通知 | Slack Incoming Webhook（標準 `net/http`） | `slack-go/slack` は却下（§7）。ペイロードは `{"text": "..."}` |
| ログ | 標準 `log/slog`（JSON 構造化ログ） | 確定 |
| DI | フレームワークなし（`internal/bootstrap` で手書き） | 確定 |
| Lint | golangci-lint v2（`.golangci.yml`） | `standard` + `revive` |
| テスト | 標準 `testing`（テーブル駆動） | testify は使わない（§7） |
| CI | GitHub Actions（`.github/workflows/ci.yml`） | format → vet → lint → test → race → integration → build |
| LLM / 外部 API | 未定 | Phase 1 では不使用。ルールベースで動かす |

### 環境変数

| 変数 | 既定値 | 用途 |
|---|---|---|
| `DATABASE_URL` | `./job-hunt-agent.db` | SQLite のファイルパス。`internal/platform/database` が `file:<path>?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)` へ組み立てる（`file:` 付き・クエリ付きの DSN を渡した場合も pragma を追記する） |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error`。未知の値は `info` として扱う |
| `SLACK_WEBHOOK_URL` | — | 案件通知の送信先。`--dry-run` なしの `notify` / `run` で**必須**。未設定なら `model.ErrMissingWebhookURL` で起動時に停止する（標準出力へフォールバックしない） |
| `SLACK_ERROR_WEBHOOK_URL` | — | ソース取得失敗の送信先。**任意**。未設定ならエラーは Slack へ送らず構造化ログにのみ残す（正常系） |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` / `GOOGLE_REFRESH_TOKEN` | — | Gmail の読み取りに使う。`sources.yaml` の `gmail` ソースが有効なら**3つとも必須**。1つでも欠けると `model.ErrMissingGoogleCredentials` で起動時に停止する。リフレッシュトークンは `job-hunt-agent auth gmail` で取得する（§10） |

Webhook URL は**ログにも DB にも出力しない**（§9）。Gmail の検索クエリ（監視対象の送信元アドレス）も同様（§10）。

## 3. パッケージ構成

```
cmd/job-hunt-agent/     エントリポイント（main のみ）
internal/
  cli/                  Cobra のサブコマンド定義
  bootstrap/            依存の組み立て（DI）
  config/               profile.yaml / sources.yaml / 環境変数の読み込みと検証
  domain/
    model/              エンティティと列挙型、センチネルエラー
    port/               application が外界へ出るための契約（interface）
  application/          ユースケース（collect / score / notify / run_pipeline）
  connector/
    fixture/            testdata から読むコネクタ（port.Connector の実装）
    gmail/              Gmail から読むコネクタ（読み取り専用。port.Connector の実装）
  parser/               抽出項目 → JobPosting の組み立て（正規化・重複キー生成）
    email/              メール本文からの項目抽出（fixture 形式 + 送信元別 extractor）
    html/               HTML からの項目抽出
  normalization/        単価・稼働・リモート・スキルの正規化
  deduplication/        収集バッチ内の重複統合
  matching/             除外判定とスコアリング
  repository/sqlite/    port.Repository の実装
  notifier/
    message/            通知本文の組み立て（stdout / slack の共通）
    stdout/             port.Notifier / port.ErrorNotifier の実装（--dry-run 用）
    slack/              port.Notifier / port.ErrorNotifier の実装（Incoming Webhook）
    noop/               port.ErrorNotifier の空実装（エラー通知先が未設定のとき）
  platform/
    database/           SQLite の接続とマイグレーション適用
    logging/            slog の組み立て
migrations/             スキーマ定義 SQL（自身を embed するパッケージ）
testdata/               架空のメール・HTML サンプル
```

### 依存方向のルール

```
cli → bootstrap → application → domain/port → domain/model
                       ↑                            ↑
        connector / repository / notifier ──────────┘
        （port の実装。application からは interface 越しにしか見えない）
```

- **`domain` は Gmail・Slack・HTML・DB の具体実装へ依存しない**
- `application` はアダプタの具体型を知らない。`port` の interface 経由でのみ利用する
- ソース固有のパーサーを共通のスコアリング処理へ混ぜない
- 外部サービスはテスト時にフェイクへ差し替えられること
- 循環依存は禁止

## 4. ドメインモデル

| 型 | 役割 |
|---|---|
| `Profile` | 利用者の希望条件。除外判定とスコアリングの基準 |
| `RawJob` | コネクタが取得した未加工の案件（本文 + 出所情報） |
| `JobPosting` | 正規化済みの案件。パイプラインの共通モデル |
| `JobSource` | 案件の紹介元。1つの `JobPosting` に複数ぶら下がる |
| `Notification` | 通知の送信試行1件ぶんの記録。成功も失敗も append する監査ログ |
| `SourceFailure` | ソース取得失敗。エラー通知の入力 |
| `CollectionRun` | ソース1つぶんの収集結果。失敗も1レコードとして残す |

### 列挙型

- `SearchStatus`: `searching` / `watching` / `paused`
- `RemoteType`: `full_remote` / `hybrid` / `onsite` / `unknown`
- `RateType`: `monthly` / `hourly` / `unknown`
- `JobStatus`: `new` / `scored` / `rejected` / `notified`
- `RunStatus`: `success` / `failed`
- `NotificationResult`: `success` / `failed`

### null の扱い

抽出できなかった数値・日付は**ポインタの nil** で保持する。ゼロ値にすると
「単価0円の案件」と「単価が読み取れなかった案件」を区別できなくなるため。

### 重複判定

`JobPosting.DedupKey` の完全一致のみで判定し、SQLite の UNIQUE 制約で担保する。
次の順で決める。

1. ソースが案件 ID を持つなら `id:<ソース名>:<案件ID>`（クラウドテックの `JA-086984` など）
2. `SourceURL` があれば `url:<URL>`
3. 無ければ `hash:<ContentHash>`（案件名 + 企業名 + 本文の SHA-256）

**案件 ID を URL より優先する**のは、クラウドテックが案件詳細 URL を持たず、
エントリー先が全案件で共通の HubSpot フォーム URL になるため。共通 URL を鍵にすると
全案件が同一 `dedup_key` になり、「衝突したら既存行を更新する」仕様により
**先に保存した案件が次の案件で上書きされて消える**（§7 の ADR）。

判定キーを1本に絞ることで、重複判定を DB の制約だけで完結させている。
案件名・単価・本文類似度による判定は Phase 5。

**同一案件を複数ソースが紹介した場合**、案件本体は1件にまとめ、`JobSource` は全件保持する。

### 再収集時の更新（重要変更）

`dedup_key` が衝突した場合、**新規登録はせず既存行の内容を最新の取得結果で更新する**。
`last_seen_at` だけを更新すると、単価が 75万 → 90万 に変わっても DB は古い値のまま残る。

ただし**採点結果（`status` / `score` / `score_reasons` / `rejection_reasons`）と `first_seen_at`
は上書きしない**。採点は `score` の領分であり、収集で巻き戻すと通知済み状態が消える。

**重要変更（material change）** は次の4項目の変化と定義する（`model.MaterialChanges`）。

| 項目 | 対象フィールド |
|---|---|
| 単価 | `rate_type` / `rate_min` / `rate_max` |
| リモート | `remote_type` / `onsite_days` |
| 開始時期 | `start_date` |
| 必須スキル | `required_skills`（並び順の違いは変更とみなさない） |

**募集終了状態は含めない**。Phase 2 の時点で募集終了を検知できるソースが存在しないため、
公開 Web コネクタが入る Phase 4 でスキーマとあわせて追加する。

## 5. 公開インターフェース

### CLI サブコマンド

| コマンド | 説明 |
|---|---|
| `init` | `config/*.example.yaml` と `.env.example` から実設定を生成（**既存ファイルは上書きしない**） |
| `auth gmail` | Gmail の読み取り専用トークンを取得する（認可 URL を表示 → 認可後リフレッシュトークンを表示） |
| `profile validate` | プロフィール設定を検証する |
| `collect [--source <name>]` | 有効なソースから案件を収集して保存する |
| `score` | 保存済み案件を再評価する |
| `notify [--dry-run]` | 閾値以上かつ未通知の案件を Slack へ通知する |
| `run [--dry-run]` | collect → score → notify を順に実行する |

グローバルフラグ: `--profile`（既定 `config/profile.yaml`）/ `--sources`（既定 `config/sources.yaml`）。

`main` は `signal.NotifyContext` で SIGINT（Ctrl-C）/ SIGTERM を受け取り、`ctx` をキャンセルする。
コネクタ・通知・DB アクセスはこの `ctx` を受け取り、中断時に途中で抜ける。

`--dry-run` あり → 標準出力。なし → Slack へ実送信。`cli.ErrDryRunRequired` は Phase 2 で廃止した。

`collect` / `score` は案件通知を行わないため、`SLACK_WEBHOOK_URL` を要求しない
（内部的には dry-run 相当で組み立てる）。ソース取得失敗を Slack へ送るのは `run`（`--dry-run` なし）。
`collect` 単体は手元での確認用と位置づけ、定期実行の入口は `run` とする（§7 の ADR）。

`notify` / `run` は、1件でも送信に失敗すると `cli.ErrNotifyFailed` で**非ゼロ終了**する
（§9「送信失敗と終了コード」）。

`status` サブコマンドは Phase 5（実行履歴の強化）で実装する。

### port（`internal/domain/port`）

```go
type Connector interface {
    Name() string
    Fetch(ctx context.Context) ([]model.RawJob, error)
}

type SaveResult struct {
    Created         bool
    MaterialChanges []string
}

type Repository interface {
    SaveJob(ctx context.Context, job *model.JobPosting) (SaveResult, error)
    ListJobs(ctx context.Context) ([]model.JobPosting, error)
    UpdateScore(ctx context.Context, job *model.JobPosting) error
    UpdateStatus(ctx context.Context, jobID int64, status model.JobStatus) error
    SaveRun(ctx context.Context, run *model.CollectionRun) error
    SaveNotification(ctx context.Context, n *model.Notification) error
    // job_id → 送信に成功した最新の通知1件。
    ListNotifiedJobs(ctx context.Context) (map[int64]NotifiedJob, error)
}

// NotifiedJob は前回通知の記録。PayloadHash で再通知の要否を、
// MaterialFields で「何が変わったか」を判定する。
type NotifiedJob struct {
    PayloadHash    string
    MaterialFields []string  // 前回通知時点のスナップショット（表示用）。旧行は空
}

// NotifyItem は「新着」と「更新」を区別するために JobPosting を包む。
type NotifyItem struct {
    Job        model.JobPosting
    Update     bool
    PrevFields []string  // 前回通知時点のスナップショット。Update のときだけ意味を持つ
}

type Notifier interface {
    Name() string  // notifications.channel に記録する
    // 成功・失敗の両方を記録として返す。dry-run 実装は nil を返す。
    Notify(ctx context.Context, items []NotifyItem) ([]model.Notification, error)
}

type ErrorNotifier interface {
    NotifyError(ctx context.Context, failures []model.SourceFailure) error
}
```

## 6. エラーハンドリング方針

`.claude/rules/go.md` の規約に従う。

### センチネルエラー

| エラー | 定義場所 | 意味 |
|---|---|---|
| `model.ErrInvalidProfile` | `internal/domain/model` | プロフィール設定が不正 |
| `model.ErrInvalidSource` | `internal/domain/model` | ソース設定が不正 |
| `model.ErrUnknownSource` | `internal/domain/model` | 指定されたソースが設定に存在しない |
| `model.ErrMissingWebhookURL` | `internal/domain/model` | 実送信に必要な Webhook URL が未設定 |
| `model.ErrMissingGoogleCredentials` | `internal/domain/model` | `gmail` ソースが有効なのに `GOOGLE_*` が揃っていない |
| `slack.ErrSend` | `internal/notifier/slack` | Slack への送信が失敗した |
| `gmail.ErrFetch` | `internal/connector/gmail` | Gmail からの取得が失敗した |
| `cli.ErrNotifyFailed` | `internal/cli` | 通知の一部または全部を送信できなかった（`notify` / `run` を非ゼロ終了させる） |
| `cli.ErrAuthFailed` | `internal/cli` | `auth gmail` の初回認証が完了しなかった |

すべて `fmt.Errorf("...: %w", err)` でラップし、`errors.Is` で判別できる状態を保つ。

`cli.ErrDryRunRequired` は Phase 2 で削除した（`--dry-run` の必須化を撤廃したため）。

### 部分失敗の扱い

- **あるコネクタが失敗しても他コネクタの処理は継続する**。失敗はサマリとログ、
  および `CollectionRun`（`status=failed` + `error_message`）に残す
- 1件の案件の解析に失敗しても、そのソースの他の案件は処理を続ける
- 実行履歴（`CollectionRun`）の保存失敗は収集全体を落とさない（副次的な記録のため）
- 抽出できない項目は null として保存し、パイプライン全体を落とさない
- **1件の通知が失敗しても残りの案件は送る**。成功した案件は `notified` として確定し、
  失敗した案件だけを次回へ持ち越す。全件を未通知へ戻すと、送信済みの案件が再送されて重複通知になる
- **中断（SIGINT / SIGTERM）されても、送信済みの記録は残す**（永続化のみ `context.WithoutCancel`。§9）
- 送信失敗は `cli.ErrNotifyFailed` として終了コードへ出す（記録は残し、次回実行で再送する）
- リトライは行わない（Phase 5）。失敗は次回実行で自然に再送される

### 実行サマリ

`log/slog` の構造化ログに、取得件数・新規件数・重複件数・更新件数・通知対象件数・送信成功件数・
失敗ソースを出力する。

## 7. 主要な設計判断（ADR 相当）

意思決定は「何を選んだか」だけでなく「何を却下したか・なぜか」まで残す。

| 日付 | 決定 | 却下した代替案 | 理由 |
|---|---|---|---|
| 2026-07-12 | 開発ワークフローを BeerSalon から移植（Issue駆動 + 常時worktree + サブエージェント分業） | 都度手作業 | 既に実運用で確立された型があり、ゼロから作る理由がない。詳細は `docs/worktree-workflow.md` |
| 2026-07-12 | ベースブランチを `develop` とする（`main` は本番） | `main` 直マージ | BeerSalon と運用を揃える。代償として `Closes #N` の自動クローズが効かず、`/merge` で明示クローズする |
| 2026-07-13 | Phase 1 は外部サービスへ接続せず、縦の線を1本通す | 先に Gmail / Slack を繋ぐ | 先に外部連携へ手を出すと認証・サイト構造・レート制限のデバッグに時間を吸われ、骨格が固まらない。ポートとアダプタの境界を先に正しく引く |
| 2026-07-13 | interface を `internal/domain/port` に集約する | 利用側（`application`）で個別に定義（`.claude/rules/go.md` の原則） | 利用側は `application` の1つだけであり、契約を1箇所へ集約したほうが依存方向を読みやすい。原則からの逸脱としてここに記録する |
| 2026-07-13 | 重複判定キーを `dedup_key` 1カラムに集約し UNIQUE 制約で担保 | URL と content_hash で条件分岐する判定ロジック | 判定が DB 制約だけで完結し、アプリ側に重複判定の分岐が要らない。Phase 5 の類似度判定を足す余地も残る |
| 2026-07-13 | HTML 解析に `golang.org/x/net/html` を採用 | `goquery` / `regexp` による解析 | goquery は依存が重い。regexp は属性順や空白の変化で壊れ、Phase 4 の実サイト対応で書き直しになる |
| 2026-07-13 | マイグレーションは自前の簡易ランナー | `golang-migrate` | ロールバック・分散ロックが現時点で不要であり、依存を1つ増やすほどの機能を必要としない |
| 2026-07-13 | テストは標準 `testing` のみ（testify を使わない） | testify | CLAUDE.md / README がテーブル駆動の標準 `testing` を確定事項としている。構造体比較の差分表示が必要になった時点で `go-cmp` を検討する |
| 2026-07-13 | フルリモートは「リモート20点 + 出社頻度10点」の計30点 | フルリモートを20点のままにする | 元仕様は両者を別配点として合計100点に積んでいるが、実際には排他でフルリモート案件が100点に到達できない。フルリモートを「出社0日 = 許容範囲内」とみなして両方を加点する |
| 2026-07-13 | 月額以外の単価（時給）は `minimum_rate` / `target_rate` と比較しない | 月間稼働時間で月額へ換算して比較する | 換算に使う時間数が案件側の実稼働と一致する保証がなく、換算値で除外すると良案件を取りこぼす。比較不能として除外も加点もせず、減点理由に残す |
| 2026-07-13 | `remote_required: true` はハイブリッド案件も除外する（「出社0日のみ許容」と解釈） | ハイブリッドを加点0で通す（現状維持） | 加点0で通すと、スキル・役割・時期・稼働の加点だけで通知閾値（`searching`=60）を超え、フルリモート必須の利用者へ出社ありの案件が届く。`ValidateProfile` が `remote_required` と `max_onsite_days > 0` の同時指定を禁じている以上、`remote_required` は「出社0日のみ許容」の意図。出社を許容する運用は `remote_required: false` + `max_onsite_days: N` で表現する |
| 2026-07-13 | `excluded_keywords` の照合対象は案件名（`title`）＋概要（`summary`）のみ | メール原文（`raw_text`）を含めた本文全体を照合する（現状維持） | 原文には「常駐必須ではありません」のような否定文や署名・引用が混ざり、部分一致で誤除外が起きる。除外は `score=0` の終端判定であり通知に一切出ないため、誤除外の損失が取りこぼしより大きい。原文まで見るなら否定表現の解釈が必要になり、それは Phase 6（抽出精度の改善）の課題 |
| 2026-07-13 | 単価の「万」表記は範囲表記を先に照合し、単一表記へフォールバックする | 単一の正規表現で「万」を任意扱いにする（現状維持） | 「65万〜90万円」のように区切りの両側へ「万」が付く表記で先頭の 65万 だけが拾われ、上限が捨てられる。`minimum_rate` を下回る扱いになり最大90万円の優良案件が誤除外される |
| 2026-07-13 | SQLite の `PRAGMA` は DSN（`_pragma=foreign_keys(1)`）へ寄せる | `db.ExecContext(ctx, "PRAGMA foreign_keys = ON")` で発行する（現状維持） | `database/sql` のプールが払い出す1コネクションにしか効かず、2本目以降で外部キーが無効に戻る（実測で `conn1=1 / conn2=0`）。DSN へ載せると全コネクションへ適用される。あわせて `busy_timeout` も設定し、定期実行が重なったときの `SQLITE_BUSY` を防ぐ |
| 2026-07-13 | Slack は標準 `net/http` で Webhook へ `{"text": "..."}` を POST する | `slack-go/slack` の導入 / Block Kit ペイロード | slack-go は Web API 向けの重い依存であり、Webhook 1本のために入れる理由がない。Block Kit は表現力の対価に組み立てと検証のコストが上がるが、要件は「本文に各項目が含まれること」だけである |
| 2026-07-13 | `Notifier.Notify` は `[]port.NotifyItem{Job, Update}` を受け取る | `[]model.JobPosting` を渡す（Issue の当初案） / `JobPosting` に一時フィールド `Update` を足す | `JobPosting` だけでは「新着」と「更新」を区別する口がなく、`【95点・更新】` を出せない。永続化モデルへ通知都合のフラグを足すと、DB に載らない状態がモデルへ混ざる |
| 2026-07-13 | `payload_hash` は**重要変更の4項目だけ**から導出する（`model.MaterialHash`） | 通知本文全体のハッシュ | 本文全体だと、加点理由の文言や配点の微調整が入るたびに再通知され、通知がノイズになる。差分検出（`MaterialChanges`）とハッシュを同じ定義から導くことで、片方だけ直して不整合になる事故も防ぐ |
| 2026-07-13 | dry-run は通知済みとして**記録しない**（`stdout` の `Notify` は nil を返す） | dry-run でも `notifications` へ書き込む | 記録すると、その後の実送信でその案件が「通知済み」として飛ばされ、送ったつもりで誰にも届かない案件が生まれる |
| 2026-07-13 | `SLACK_WEBHOOK_URL` 未設定（`--dry-run` なし）は起動時にセンチネルで停止する | 標準出力へフォールバックする | 「送ったつもりで送られていない」ほうが、起動に失敗するより損失が大きい。通知は届かなければ価値が0 |
| 2026-07-13 | Slack の送信エラーは `*url.Error` を剥がしてから返す（`slack` パッケージの出口） | `fmt.Errorf("...: %w", err)` でそのままラップする | `net/http` の送信エラーは `*url.Error` で、`Error()` が **Webhook URL 全体を含む**。素直にラップすると、既存の `collect_jobs.go` の `err.Error()` ロギングと `collection_runs.error_message` / `notifications.error_message` への永続化が受け皿になり、秘密情報がログと DB へ書き出される |
| 2026-07-13 | `score` は `status=notified` を `scored` へ上書きしない | 無条件に `scored` を代入する（現状維持） | `run` は collect → score → notify の順に走るため、上書きすると2回目の `run` で `notified` が消える。再通知の判定は `notifications` テーブルで行うため再通知バグにはならないが、「送信成功した案件は `notified`」という状態が意味を失う。除外条件に触れた場合は `rejected` が優先される |
| 2026-07-13 | エラー通知を `port.ErrorNotifier` として別 interface に切り、収集の最後に1回だけ送る | 案件通知の `Notifier` に相乗りさせる / 失敗のたびに送る | 宛先（`SLACK_ERROR_WEBHOOK_URL`）が案件通知と別であり、契約も入力（`SourceFailure`）も異なる。失敗のたびに送ると、ソースが軒並み落ちたときに通知が埋まる |
| 2026-07-13 | **通知履歴の永続化だけ `context.WithoutCancel(ctx)` を使う**（送信の `ctx` はキャンセル可能なまま） | 送信と同じ `ctx` で `SaveNotification` / `UpdateStatus` を呼ぶ（現状維持） | 中断（Ctrl-C / SIGTERM）時、`slack.Notifier` は送信済みの記録を返して抜けるが、キャンセル済み `ctx` では `ExecContext` が必ず失敗し記録が残らない。結果「Slack には届いたのに通知済みにならない」案件が生まれ、次回実行で再送される（本 PR の目的である重複通知の抑止を自ら破る）。**送信は中断できるが、送信済みの記録は必ず残す**を不変条件とする |
| 2026-07-13 | `ListNotifiedJobs`（旧 `ListNotifiedJobIDs`）の `ORDER BY` は `id ASC` のみ | `ORDER BY sent_at ASC, id ASC`（現状維持） | `sent_at` はアプリ側の時刻由来でテキストとして格納され、タイムゾーン表記の混在や時刻の巻き戻りで辞書順が保存順と食い違いうる。古い `payload_hash` が最新として残ると、変更済みの案件が「変更なし」と誤判定されて再通知されない。`id` は AUTOINCREMENT で単調増加するため、第1キーを `sent_at` にする実益がない |
| 2026-07-13 | 送信失敗（`NotifySummary.FailedCount > 0`）は `cli.ErrNotifyFailed` で**非ゼロ終了**する | exit 0 のまま標準出力にだけ「送信失敗 N件」と出す（現状維持） | GitHub Actions の schedule で回す前提であり、失敗が赤くならないと誰にも届いていないことに気づけない。案件の保存・採点は完了しているためロールバックはせず、失敗した案件は次回実行で再送される（終了コードは「気づかせる」ためだけに使う） |
| 2026-07-13 | Slack へは案件1件ごとに1秒（`slack.defaultSendInterval`）空けて送る | 待ちなしで連射する（現状維持） / 429 を検出したら以降を打ち切る / 複数案件を1メッセージへまとめる | Incoming Webhook は概ね 1 msg/sec で、初回収集のように通知が10件以上並ぶと後半が 429 で落ちる。429 は「失敗して次回再送」では解けない（次回も同じ速度で送り同じ位置で失敗する）。打ち切りは通知の遅延を生み、1メッセージへの集約は案件ごとの可読性を失う。ウェイトは `time.After` + `ctx.Done()` の `select` で待ち、中断に即応する。間隔はコンストラクタ（`slack.WithSendInterval`）から差し替えられる |
| 2026-07-13 | 「更新」通知への**変更内容の表示は Phase 5 へ回す**（→ Issue #13 で前倒し実装。下2行の ADR で置き換え） | Phase 2 で `notifications` へ `MaterialFields` のスナップショットを保存し、差分を本文へ載せる | 旧値は `notify` の時点で DB から消えており、差分を出すにはスキーマ追加（前回スナップショットの保存）が必要になる。Phase 2 の受入条件は「重要変更を見逃さず再通知する」であり、変更内容の表示はその上に載る改善。スキーマ変更を伴う以上、実行履歴を強化する Phase 5 でまとめて扱う |
| 2026-07-13 | 差分表示は**表示用スナップショット**（`message.Snapshot` → `notifications.material_fields`）を別に保存して行い、`payload_hash` の定義（`model.MaterialHash`）は**一切変えない** | `model.MaterialFields` をそのまま保存して素で表示する / `materialFields` の値表現自体を日本語化して単一定義のまま使う | `model.MaterialFields` はハッシュの入力であり、値が `monthly 750000〜850000` / `full_remote` のような内部表現。そのまま出すと利用者向けの Slack 通知に内部表現が露出する。かといって値を日本語化すると `MaterialHash` の入力が変わり、**通知済みの全案件が次回実行で一斉に「更新」再通知される**（中身は何も変わっていないのに）。再通知の判定（ハッシュ・不変）と差分の表示（スナップショット・表示層）へ責務を割ることで、本文の `単価：` 行と `変更：` 行が同じフォーマッタから出て表記も揃う。代償として項目定義が2箇所に増えるため、ラベル集合の一致を UT（`TestSnapshotLabelsMatchMaterialFields`）で担保する |
| 2026-07-13 | `MaterialHash` の値を golden 値として UT で固定する（`TestMaterialHashGolden`） | ハッシュの安定性（同じ入力で同じ値）だけをテストする（現状維持） | `payload_hash` は `notifications` へ永続化されており、ハッシュの入力を変えた瞬間に既存の全レコードと一致しなくなって一斉再通知が起きる。「同じ入力で同じ値」のテストは定義変更を検知できない。重要変更の項目を意図して増やすときは、一度だけ再通知されることを承知のうえで golden 値を更新する |
| 2026-07-13 | `collect` 単体ではエラー通知を Slack へ送らない（`dryRun=true` で組み立てる） | `buildNotifiers` の `DryRun` 分岐を案件通知とエラー通知で分け、`collect` でもエラー通知だけ実送信にする | 定期実行の入口は `run` であり、`collect` 単体は手元での確認用と位置づける。`collect` を実送信として組み立てると、通知を行わないコマンドの副作用として Slack へ投稿が飛び、手元で試すたびにチャンネルが汚れる。`collect` だけを定期実行する運用が現実に出てきた時点で見直す |
| 2026-07-13 | 「基本リモート」「原則リモート」「リモート中心」を **hybrid** に分類する | `unknown` のまま扱う（現状維持） | 実エージェント（フォスターネット）の `※基本リモート（必要に応じて出社あり）` がどのパターンにも当たらず `unknown` へ落ちていた。`reject` は「抽出漏れで良案件を落とさない」方針から `unknown` を除外しないため、**`remote_required: true`（出社0日のみ許容）の利用者へ出社を伴う案件がそのまま通知される**。出社日数が読めない場合は `onsite_days` を nil のままとし、ハイブリッドとして除外する |
| 2026-07-13 | 単価は「円」を伴わない通貨記号表記（`～￥850,000/月`）も月額として読む。**範囲表記を先に照合し、単一表記へフォールバックする** | `([\d,]+)\s*円` のまま「円」を必須にする（現状維持） / 「円」表記を先に試し、無ければ通貨記号表記へ切り替える either/or 方式 / 全マッチを集めて先頭と末尾を min/max に採る | 実エージェント（クラウドテック）の単価表記が `～￥850,000/月程度（週5日稼働換算・税別）` で、「円」も「万」も含まない。読めないと `RateTypeUnknown` になり、`minimum_rate` による除外も `target_rate` による加点も**一切効かない**。ただし either/or 方式は `￥600,000～1,000,000円` で「円」側だけを見て下限を捨て、`min=max=1,000,000` と過大評価する（60万の案件が最低単価を通過し 20点を誤加点）。全マッチの先頭・末尾方式は `月額 ￥850,000（交通費別途 500円）` で `min=850,000 / max=500` と逆転する。`manYen` と同じ「範囲を先に、単一へフォールバック」構造に揃えるのが唯一どちらも壊さない |
| 2026-07-13 | 稼働日数は「週」を伴わない「5日」も読む。範囲チェック（1〜7）に加え、**「日」の直後に数字を許さない** | 正規表現から「週」を外すだけにする / 範囲チェックだけで守る | クラウドテックの `・稼働：5日 / フルリモート` は「週」を伴わず、必須にすると稼働日数が読めない。一方で「週」を単に任意化すると `月20日稼働` を1桁ずつ拾って `min=2 / max=0` という不整合な組で返す。範囲チェックはこれを弾けるが、**`1日8時間` は「週1日」として通過してしまう**（1 は範囲内）。「日」の直後に数字を許さないことで日次の労働時間表記を除く。稼働日数は加点5点のみで除外に効かないため、読めない場合は nil（加点0）に倒すのが「抽出漏れで良案件を落とさない」方針と整合する |
| 2026-07-13 | スキルは**辞書ベースで自然文から抽出する**（`normalization.ExtractSkills`） | `SplitList` の区切り文字分割だけで済ませる（現状維持） / 生成 AI で抽出する | 実メールのスキルは `・Java、JavaScriptでのWebアプリケーション開発経験（5年以上目安）` のような文章であり、区切り文字で割ると文がそのまま1スキルとして残る。`matching.intersect` は完全一致で照合するため、**得意スキルの25点がほぼ死ぬ**。生成 AI は「生成 AI API を必須にしない」方針に反する。既存の `skillAliases` を辞書として流用し、ASCII の別名だけ単語境界つき正規表現にする（部分一致だと `go` が `Google` に、`ts` が `sports` に当たる。素の `next` は `next step` から `Next.js` を誤抽出するため辞書に持たない）。**限界: 辞書に無いスキルは抽出できない**。利用者が `preferred_skills` に辞書外の語（`Vue` / `Kotlin` 等）を書いても一致しないため、`profile.preferred_skills` を辞書へ合流させることを Phase 3 で検討する |
| 2026-07-13 | `ExtractSkills` は出現順で返す（並び順を決定的にする） | map の反復順のまま返す | `MaterialChanges` / `MaterialHash` は比較前に `slices.Sort` するため**再通知は起きない**が、`required_skills` はそのまま DB へ保存され通知本文にも出る。順序が揺れると内容が同じ案件でも収集のたびに UPDATE が走り、本文の表示順も安定しない |
| 2026-07-13 | `payload_hash` の導出（`model.MaterialHash`）と**スナップショットの生成（`message.Snapshot`）**は当面 Notifier アダプタ側に置く | `port.NotifyItem` に `PayloadHash` を持たせて `application` が詰める / `Notifier` は送否だけ返し `application` が `model.Notification` を組み立てる | 再通知の判定基準はユースケースの責務であり、層としては `application` 側が素直。ただし現状 Notifier は `slack` / `stdout` の2実装で壊れておらず、動く構造を組み替える価値が今はない。**Notifier が増える Phase 3 で再検討する**（実装が散ると片方だけ古い定義を使う事故が起きうる）。判定（`MaterialHash`・domain）と表示（`Snapshot`・adapter）が対で使われるのに層が割れている点も、この再検討に含める |
| 2026-07-13 | 正規化は「出社0日」を `(full_remote, nil)` へ寄せる（`OnsiteDays` に 0 を残さない） | 「週0日出社」を `(full_remote, &0)` として出社日数を保持する（現状維持） | 同じ「フルリモート」が `(full_remote, nil)` と `(full_remote, &0)` の2通りで表現でき、`model.materialRemote`（ハッシュの入力）は出社日数まで見て両者を別物と扱うのに、`message.formatRemote`（表示）はフルリモートなら出社日数を捨てる。結果、ソース側の表記が「フルリモート」↔「週0日出社」で揺れただけで再通知が起き、しかも差分行が出ないため**見出し以外まったく同じ通知**が届く。正規化の時点で表現を1本化すれば、ハッシュ側・表示側のどちらも触らずに解消する（`MaterialHash` の golden 値も変わらない）。不変条件「ハッシュが変わるなら必ず差分を1行以上出せる」は `message` のプロパティテスト（`TestUpdateAlwaysExplainsItself`）で総当たり検証する |
| 2026-07-13 | Gmail は `golang.org/x/oauth2` + 標準 `net/http` で REST を直接叩く | `google.golang.org/api/gmail/v1` の導入 | 使うのは `messages.list` と `messages.get` の2エンドポイントだけであり、grpc を含む重い依存ツリーを引き込む対価に見合わない（`slack-go/slack` を却下して Webhook を `net/http` で叩いたのと同じ論理）。トークンの更新だけは自前実装が無意味なので `oauth2` に任せる。`golang.org/x/oauth2/google` すら使わない（GCE メタデータサーバー検出のため `cloud.google.com/go/compute/metadata` を引き込む。必要なのは URL 2本だけなので `gmail.Endpoint` として自前で持つ） |
| 2026-07-13 | `dedup_key` に `id:<ソース名>:<案件ID>` 形式を足し、**URL より優先する** | `url:` / `hash:` の2択のまま（現状維持） | クラウドテックは案件詳細 URL を持たず、エントリー先が**全案件で共通の HubSpot フォーム URL**。これを鍵にすると全案件が同一 `dedup_key` になり、「衝突したら既存行を更新する」仕様により**先に保存した案件が次の案件で上書きされて消える**（1日3〜5件届くソースで、DB に1件しか残らない）。`hash:` へ倒す案もあるが、本文が1文字でも変われば別案件になり再通知が止まらない。案件 ID にソース名を混ぜるのは、別ソースが同じ ID 体系（連番など）を使ったときの衝突を避けるため |
| 2026-07-13 | Gmail の検索は**送信元アドレスのホワイトリスト**で組む（`from:(A OR B) newer_than:Nd`） | `案件` `単価` などのキーワード検索 / ラベルでの絞り込み | 実受信箱をキーワードで検索すると、マイナビ転職の正社員求人・タウンワークのアルバイト求人・ビズリーチのスカウトが大量にヒットし、フリーランス案件が埋もれる（実測）。ラベルは利用者の手作業に依存し、設定漏れで無音になる |
| 2026-07-13 | Phase 3 の対象を**クラウドテックとフォスターネットの2社に絞る** | 案件メールを送る5社すべてに対応する | 残る3社は採点に必要な情報を持たない。**Remogu** は本文に案件名・企業名・稼働形態しかなく単価もスキルも無い（採点不能。詳細は Web 取得が要るため Phase 4 の領分）。**フリーランスハブ**は1メールに1〜7案件を載せ、「1 RawJob = 1 JobPosting」というモデルの前提を壊す（別 Issue）。**ギークス**はイベント告知・営業メールのみで案件データを含まない |
| 2026-07-13 | 送信元別の extractor を `internal/parser/email` 内で `RawJob.Sender` により振り分ける | 共通の「ラベル: 値」規則へ寄せる / コネクタ側でソースごとにパーサーを持つ | 実メールの書式は社ごとに異なり共通規則へ寄せられない（クラウドテックはラベルの**次の行**が値、フォスターネットは**同じ行**に値が続く。「稼動日数」のような異表記もある）。一方で抽出はメール本文の解釈であってコネクタ（取得）の責務ではなく、Gmail 以外の経路で同じメールが来ても同じ抽出が効くべき。fixture 形式の `Parse` は残し、対応する送信元だけ `Extract` が上書きする |

## 8. スコアリング

### 除外条件（1つでも該当したら `rejected`。点数は付けない）

- 月額単価が `minimum_rate` を下回る（**月額表記の案件のみ**。時給案件は比較しない）
- `remote_required` かつ案件が `onsite` **または `hybrid`**
  （`remote_required` は「出社0日のみ許容」の意味。週N日出社のハイブリッドも除外する。
  出社を許容する運用は `remote_required: false` + `max_onsite_days: N` で表現する）
- `excluded_keywords` が**案件名（`title`）または概要（`summary`）**に含まれる
  （メール原文 `raw_text` は照合しない。否定文・署名・引用での誤除外を避けるため）
- `contract_types` に無い契約形態

### 配点（合計100点）

| 項目 | 配点 |
|---|---|
| 希望単価（`target_rate`）以上 | 20 |
| フルリモート | 30（リモート20 + 出社頻度10） |
| ハイブリッドかつ出社頻度が `max_onsite_days` 以内 | 10 |
| 得意スキル（`preferred_skills`）との一致 | 最大25（一致数 / 得意スキル総数で按分） |
| 希望する役割（`desired_roles`）との一致 | 最大10（同上） |
| 参画希望時期（`available_from`）以降の開始 | 10 |
| 希望稼働日数（`preferred_work_days`）が案件の範囲内 | 5 |

値が unknown / nil の項目は**加点0・除外しない**。抽出漏れを理由に良案件を落とすほうが、
点が伸びずに埋もれるより損失が大きいため。

加点理由（`score_reasons`）と減点理由（`rejection_reasons`）を両方保持し、通知に表示する。

### 通知閾値

| 求職状態 | 閾値 | 挙動 |
|---|---|---|
| `searching` | 60 | 通常の閾値で通知 |
| `watching` | 80 | 高スコア案件のみ通知 |
| `paused` | — | 収集も通知も行わない |

`profile.notification_threshold` に 1〜100 を指定すると、状態別の既定値を上書きする。
`paused` は値によらず通知しない。

閾値は実データが溜まった段階（Phase 6）でスコア重みとあわせて調整する暫定値。

---

## 9. 通知（Phase 2）

### 送信先

| 種別 | 宛先 | 実装 |
|---|---|---|
| 案件通知 | `SLACK_WEBHOOK_URL` | `notifier/slack.Notifier` |
| エラー通知（ソース取得失敗） | `SLACK_ERROR_WEBHOOK_URL` | `notifier/slack.ErrorNotifier` |
| `--dry-run` | 標準出力 | `notifier/stdout`（案件・エラーの両方） |
| エラー通知先が未設定 | 送らない（ログのみ） | `notifier/noop.ErrorNotifier` |

ペイロードは `{"text": "<stdout と同じ整形テキスト>"}`。本文の組み立ては `notifier/message` に集約し、
stdout と slack の双方から使う（通知実装どうしが依存し合わないため）。

HTTP クライアントは 10 秒のタイムアウトを持ち、`http.NewRequestWithContext` で `ctx` の
キャンセルに追随する。**通知対象が0件なら HTTP リクエストを送らない**（無音）。

### 送信レート

案件は**1件 = 1 POST**で送り、**1件ごとに1秒空ける**（`slack.defaultSendInterval`）。
Incoming Webhook が概ね 1 msg/sec で、超過すると 429 を返すため。
最後の送信のあとは待たない。

ウェイトは `time.After` + `ctx.Done()` の `select` で待つ（中断に即応する。`time.Sleep` にしない）。
間隔は `slack.WithSendInterval` でコンストラクタから差し替えられる（テストが実時間を待たないため）。

リトライは Phase 5。

### 中断（SIGINT / SIGTERM）時の扱い

**送信は中断できるが、送信済みの記録は必ず残す。**

`slack.Notifier` は中断時、そこまでの送信記録を返して抜ける。`application.Notifier` は
その記録の永続化（`SaveNotification` / `UpdateStatus`）だけを `context.WithoutCancel(ctx)` で行う。
送信と同じ ctx で永続化すると、キャンセル済み ctx では `ExecContext` が必ず失敗して記録が残らず、
「Slack には届いたのに通知済みにならない」案件が次回実行で再送される。

### 送信失敗と終了コード

1件でも送信に失敗したら（`NotifySummary.FailedCount > 0`）、`notify` / `run` は
`cli.ErrNotifyFailed` を返して**非ゼロ終了**する。定期実行（GitHub Actions の schedule）で
失敗が赤くならないと、通知が届いていないことに気づけないため。

案件の保存・採点は完了しているためロールバックはしない。失敗した案件は `status` が変わらず、
次回実行で再送される。

### 通知済み管理

`notifications` は**送信試行ごとに1行 append する監査ログ**（UNIQUE 制約なし。失敗 → 再送の履歴を残す）。

判定は `ListNotifiedJobs`（`result = 'success'` の最新行の `job_id → NotifiedJob`）で行う。

| 状態 | 挙動 |
|---|---|
| 未通知（`notifications` に成功行がない） | **通知する**（`【95点・新着】`） |
| 通知済みかつ `payload_hash` が同じ | 通知しない |
| 通知済みかつ `payload_hash` が異なる（= 重要変更あり）かつ再評価後も閾値以上 | **「更新」として通知する**（`【95点・更新】`） |

判定に使う「最新の成功行」は `ORDER BY id ASC` で決める（`sent_at` はアプリ側時刻由来の
テキストであり、辞書順が保存順と一致する保証がない）。

送信に成功した案件は `notifications`（`result=success`）へ記録し、`job_postings.status` を
`notified` にする。失敗した案件は `result=failed` + `error_message` を記録し、`status` は変えない
（次回実行で再送される）。

### 「更新」通知の差分表示

「更新」通知は見出しの直下へ**何がどう変わったか**を載せる。変わっていない項目は出さない。

```
【95点・更新】Java／AWS 基盤改善案件
変更：
・単価：750000〜850000円 → 900000〜1000000円
・リモート：フルリモート → ハイブリッド（週2日出社）
単価：900000〜1000000円　稼働：週3日　開始：2026-09-01
…
```

旧値は `notify` の時点で `job_postings` から上書き済みのため、通知の送信時に
**表示用のスナップショット**（`notifier/message.Snapshot`）を `notifications.material_fields`
（`\x1f` 区切り）へ保存し、次の「更新」通知でそこから復元する。

**スナップショットは `payload_hash`（`model.MaterialHash`）とは別に持つ。** 再通知の判定は
ハッシュ、差分の表示はスナップショットと役割を分け、ハッシュの入力（内部表現）を
表示都合で変えない（§7 の ADR）。

判定と表示を別に持つ以上、**「ハッシュが変わったのに差分が1行も出せない」状態を作ってはならない**
（見出し以外まったく同じ「更新」通知が届き、この機能の目的を自ら破る）。これを不変条件として、
`message` のテストで担保する。

| テスト | 担保する内容 |
|---|---|
| `TestSnapshotLabelsMatchMaterialFields` | 項目とラベルが両者でずれていない |
| `TestUpdateAlwaysExplainsItself` | `MaterialHash` が変わるなら `Format` は必ず1行以上の差分を出す（正規化が返しうる状態の総当たり） |

ラベルの一致だけでは**値の識別力の差**を検知できない。実際、`normalization.Remote` が
「週0日出社」に `(full_remote, &0)` を返していた頃は、ハッシュは変わるのに表示は
どちらも「フルリモート」で差分が0行になった（§7 の ADR で正規化側を1本化して解消）。

`material_fields` を持たない行（この機能より前に通知した案件）は空で返り、
その案件の初回の「更新」通知だけ差分行を出さず、見出しのみへフォールバックする。

### 秘密情報の扱い

**Webhook URL をログにも DB にも出力しない。**

`net/http` の送信エラーは `*url.Error` であり、`Error()` が URL 全体を含む。`slack` パッケージの
出口で `errors.As` により原因だけを取り出して詰め替える。HTTP エラーはステータスコードのみを返し、
レスポンスボディもログへ出さない。

この経路を塞がないと、`collect_jobs.go` の `err.Error()` ロギングと
`collection_runs.error_message` / `notifications.error_message` への永続化が受け皿になり、
Webhook URL がログと DB に残る。

---

## 10. Gmail（Phase 3）

### スコープと権限

要求するのは **`gmail.readonly` のみ**。削除・アーカイブ・既読化・ラベル変更・返信は行わない
（§1「やらないこと」）。

### セットアップ

1. Google Cloud Console でプロジェクトを作り、**Gmail API を有効化**する
2. OAuth クライアント ID（種類: **デスクトップアプリ**）を発行し、
   `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` を `.env` へ設定する
3. `job-hunt-agent auth gmail` を実行する
   - ローカルの空きポートで待受け、**認可 URL を標準出力へ表示する**（ブラウザは自動で開かない。
     SSH 越しやコンテナ内で実行しても手順が変わらないようにするため）
   - 表示された URL をブラウザで開いて認可すると、リフレッシュトークンが標準出力へ出る
     → `GOOGLE_REFRESH_TOKEN` へ貼る
4. `config/sources.yaml` の `gmail` ソースを `enabled: true` にする

`AccessTypeOffline` + `ApprovalForce` を付けているのは、これが無いと2回目以降の認可で
Google が `refresh_token` を返さないため。CSRF 対策として `state` を照合する。

### 取得

| 項目 | 内容 |
|---|---|
| エンドポイント | `users.messages.list` → `users.messages.get`（標準 `net/http`） |
| 検索クエリ | `from:(<senders を OR で連結>) newer_than:<newer_than>` |
| 本文 | `payload.parts` から **text/plain を優先**（無ければ text/html）。base64url デコード |
| 上限 | `max_results`（既定 100）。1ページ 100 件でページングする |
| タイムアウト | 30 秒 |

`RawJob` には `Format: "email"` / `Sender` / `ReceivedAt`（`internalDate`）/
`ExternalID`（Gmail のメッセージ ID）を詰める。

**送信元で絞る**のが要（§7 の ADR）。キーワード検索にすると転職サイトの求人メールが
案件メールを圧倒する。

### 対応するエージェント

本文から採点に必要な項目（単価・稼働・リモート・スキル）を抽出できるのは現状この2社のみ。

| | クラウドテック | フォスターネット |
|---|---|---|
| 送信元 | `alliance-crowdtech@crowdworks.co.jp` | `careers.desk.haishin@foster-net.co.jp` |
| ラベル形式 | `■案件名` の**次の行**が値 | `■案件名：値`（同じ行） |
| 単価 | `・金額：～￥850,000/月程度` | `■金額：～75万円(税抜)` |
| 稼働 | `・稼働：5日 / フルリモート`（稼働とリモートが同居） | `■稼動日数：平日週5日`（「稼**動**」） |
| リモート | `フルリモート` / `一部リモート` / `常駐` | `■場所：六本木駅 ※基本リモート（必要に応じて出社あり）` |
| スキル | `≪必須経験・スキル≫` 配下の自然文 | `＜必須＞` 配下の自然文 |
| 重複キー | `■案件ID：JA-086984` → `id:` | 案件掲載 URL → `url:` |

対象外にした3社（Remogu / フリーランスハブ / ギークス）とその理由は §7 の ADR を参照。

### 秘密情報の扱い

**Gmail の検索クエリをログにも DB にも出力しない。**

クエリには監視対象の送信元アドレスが載る。`net/http` の送信エラーは `*url.Error` で
`Error()` がリクエスト URL 全体を含むため、`gmail` パッケージの出口で `errors.As` により
原因だけを取り出して詰め替える（`slack` パッケージと同じ扱い）。HTTP エラーはステータスコードのみを返し、
**レスポンスボディも出さない**（Google のエラーがリクエスト内容を反射することがある）。

401 / 403 のときだけ「`auth gmail` でトークンを取り直す」旨をメッセージに含める
（リフレッシュトークンの失効は運用中に必ず起きるため）。

### 失敗時の扱い

既存方針どおり、**Gmail が失敗しても他コネクタの収集は継続する**。失敗は `CollectionRun`
（`status=failed`）とサマリに残り、`run`（`--dry-run` なし）ならエラー通知先へ送られる。

`GOOGLE_*` が1つでも欠けた状態で `gmail` ソースが有効なら、`model.ErrMissingGoogleCredentials`
で**起動時に停止する**。黙って0件成功にすると「収集したつもりで1件も取れていない」事故になる
（`SLACK_WEBHOOK_URL` 未設定で停止するのと同じ方針）。
