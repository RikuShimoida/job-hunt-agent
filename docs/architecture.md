# アーキテクチャ

job-hunt-agent の構成・設計判断を集約するドキュメント。
`/sync-docs` スキルの同期対象であり、**コードを読まなくてもここを見れば仕様が分かる状態**を保つ。

> **現状: Phase 0 / Phase 1 完了。** 外部サービスへ接続しない縦切り MVP が通っている。
> Phase 2 以降（Slack / Gmail / 公開 Web）は未実装。未実装の領域は「未定義」と明記し、
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
| 2 | Slack Incoming Webhook 接続と通知済み管理 | 未着手 |
| 3 | Gmail 読み取り専用 OAuth と案件メール解析 | 未着手 |
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
| `SLACK_WEBHOOK_URL` | — | Phase 2 以降。Phase 1 では未使用 |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` / `GOOGLE_REFRESH_TOKEN` | — | Phase 3 以降。Phase 1 では未使用 |

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
  connector/fixture/    testdata から読むコネクタ（port.Connector の実装）
  parser/               抽出項目 → JobPosting の組み立て（正規化・重複キー生成）
    email/              メール本文からの項目抽出
    html/               HTML からの項目抽出
  normalization/        単価・稼働・リモート・スキルの正規化
  deduplication/        収集バッチ内の重複統合
  matching/             除外判定とスコアリング
  repository/sqlite/    port.Repository の実装
  notifier/stdout/      port.Notifier の実装（dry-run 用）
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
| `Notification` | 送信済み通知の記録（Phase 1 では書き込まない） |
| `CollectionRun` | ソース1つぶんの収集結果。失敗も1レコードとして残す |

### 列挙型

- `SearchStatus`: `searching` / `watching` / `paused`
- `RemoteType`: `full_remote` / `hybrid` / `onsite` / `unknown`
- `RateType`: `monthly` / `hourly` / `unknown`
- `JobStatus`: `new` / `scored` / `rejected` / `notified`
- `RunStatus`: `success` / `failed`

### null の扱い

抽出できなかった数値・日付は**ポインタの nil** で保持する。ゼロ値にすると
「単価0円の案件」と「単価が読み取れなかった案件」を区別できなくなるため。

### 重複判定

`JobPosting.DedupKey` の完全一致のみで判定し、SQLite の UNIQUE 制約で担保する。

- `SourceURL` があれば `url:<URL>`
- 無ければ `hash:<ContentHash>`（案件名 + 企業名 + 本文の SHA-256）

判定キーを1本に絞ることで、重複判定を DB の制約だけで完結させている。
案件名・単価・本文類似度による判定は Phase 5。

**同一案件を複数ソースが紹介した場合**、案件本体は1件にまとめ、`JobSource` は全件保持する。

## 5. 公開インターフェース

### CLI サブコマンド

| コマンド | 説明 |
|---|---|
| `init` | `config/*.example.yaml` と `.env.example` から実設定を生成（**既存ファイルは上書きしない**） |
| `profile validate` | プロフィール設定を検証する |
| `collect [--source <name>]` | 有効なソースから案件を収集して保存する |
| `score` | 保存済み案件を再評価する |
| `notify --dry-run` | 閾値以上の案件を標準出力へ表示する |
| `run --dry-run` | collect → score → notify を順に実行する |

グローバルフラグ: `--profile`（既定 `config/profile.yaml`）/ `--sources`（既定 `config/sources.yaml`）。

`main` は `signal.NotifyContext` で SIGINT（Ctrl-C）/ SIGTERM を受け取り、`ctx` をキャンセルする。
コネクタ・通知・DB アクセスはこの `ctx` を受け取り、中断時に途中で抜ける。

Phase 1 では Slack への実送信が未実装のため、`notify` / `run` は `--dry-run` が必須。
省略すると `cli.ErrDryRunRequired` を返す。

`status` サブコマンドは Phase 5（実行履歴の強化）で実装する。

### port（`internal/domain/port`）

```go
type Connector interface {
    Name() string
    Fetch(ctx context.Context) ([]model.RawJob, error)
}

type Repository interface {
    SaveJob(ctx context.Context, job *model.JobPosting) (created bool, err error)
    ListJobs(ctx context.Context) ([]model.JobPosting, error)
    UpdateScore(ctx context.Context, job *model.JobPosting) error
    SaveRun(ctx context.Context, run *model.CollectionRun) error
}

type Notifier interface {
    Notify(ctx context.Context, jobs []model.JobPosting) error
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
| `cli.ErrDryRunRequired` | `internal/cli` | Slack 送信が未実装のため `--dry-run` が必要 |

すべて `fmt.Errorf("...: %w", err)` でラップし、`errors.Is` で判別できる状態を保つ。

### 部分失敗の扱い

- **あるコネクタが失敗しても他コネクタの処理は継続する**。失敗はサマリとログ、
  および `CollectionRun`（`status=failed` + `error_message`）に残す
- 1件の案件の解析に失敗しても、そのソースの他の案件は処理を続ける
- 実行履歴（`CollectionRun`）の保存失敗は収集全体を落とさない（副次的な記録のため）
- 抽出できない項目は null として保存し、パイプライン全体を落とさない

### 実行サマリ

`log/slog` の構造化ログに、取得件数・新規件数・重複件数・通知件数・失敗ソースを出力する。

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
