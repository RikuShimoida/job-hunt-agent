---
name: set-conditions
description: 案件探しの希望条件（config/profile.yaml）をインタビュー形式で更新する。現在値を見せながら1項目ずつ確認し、差分を提示して承認後に保存する。上書き前に旧条件を履歴へ退避する。
when_to_use: 「希望条件を変えたい」「条件を更新して」「set-conditions」「単価ラインを上げたい」「稼働日数を変えたい」などの発言時
context: fork
agent: go-engineer
---

## 希望条件のインタビュー更新

`config/profile.yaml` をインタビュー形式で更新する進行役。**検証・履歴退避・原子的書き込みの
中核は Go 側の `profile apply` が持つ**。このスキルは現在値を見せて質問し、新 profile を組み立てて
`profile apply` へ渡すことに徹する（受入条件は `go test` が担保する。ここではロジックを再実装しない）。

引数: `$ARGUMENTS`（任意。`--profile <path>` で対象ファイルを指定できる。既定 `config/profile.yaml`）。

### 手順

#### 1. 現在値を読む

- 対象パス（既定 `config/profile.yaml`）を Read する。
- ファイルが無ければ「初回作成」と伝え、`config/profile.example.yaml` を土台にする。
- **YAML 全文をそのまま土台として保持する**（コメント・項目順を壊さないため。`profile apply` は
  受け取ったバイト列をそのまま保存する。再整形しない）。

#### 2. カテゴリごとに現在値を見せて確認する

各カテゴリについて `AskUserQuestion` で確認する。**必ず現在値を選択肢か説明文に含め、「現在値のまま」を
選べるようにする**（全項目を毎回入力させない）。順序:

1. **求職状態** `search_status`（searching / watching / paused）
2. **単価ライン** `minimum_rate` / `target_rate`（円・月。`minimum_rate <= target_rate`）
3. **稼働・リモート** `preferred_work_days`（0〜7）/ `remote_required` / `max_onsite_days`（0〜7）
   - **`remote_required: true` と `max_onsite_days > 0` は同時指定できない**。
     出社を許容するなら `remote_required: false` + `max_onsite_days: N` を案内する。
4. **スキル・役割** `preferred_skills` / `desired_roles`（追加・削除）。`required_skills` は空にできない。
5. **その他（必要に応じて）** `available_from` / `contract_types` / `excluded_keywords` /
   `monthly_hours_min` / `monthly_hours_max` / `notification_threshold`

回答は土台の YAML の該当キーだけ書き換える。触れなかったキーは元のまま残す。

#### 3. 差分を提示して承認を得る

- 変更した項目について**旧→新**を並べて提示する（変えていない項目は出さない）。
- `AskUserQuestion` で「この内容で保存する / やり直す」を確認する。やり直すなら 2 へ戻る。

#### 4. 保存する（`profile apply` を呼ぶ）

- 組み立てた新 profile YAML をスクラッチパッドの一時ファイルへ Write する。
- `job-hunt-agent profile apply --from <一時ファイル>`（対象を変えたなら `--profile <path>` も付ける）を
  Bash で実行する。
- **成功**: 出力の「退避: …」「保存: …」をユーザーへ伝える。
- **検証エラー**（`model.ErrInvalidProfile` でラップされて非ゼロ終了）: どの項目がなぜ不正かを
  エラーメッセージから読み、ユーザーへ提示して 2 の該当項目へ戻る。**現行 `profile.yaml` は
  変更されていない**旨を添える（apply は検証 → 退避 → 書き込みの順で、検証失敗時は何も書かない）。

### 過去条件を見る

- 一覧: `job-hunt-agent profile history`
- 内容表示: `job-hunt-agent profile history --show <ID>`（ID は一覧に出るタイムスタンプ）
- 戻したいときは、表示した内容を一時ファイルへ書いて `profile apply --from` で再適用する
  （履歴からの直接復元コマンドは持たない。復元も「新しい適用」として履歴に1件残る）。

### やらないこと

- 検証・履歴退避・書き込みロジックをこのスキル内で再実装しない（`profile apply` に委ねる）。
- `collect` / `score` / `notify` / `run` は起動しない（条件更新のみ。次回の実行から新条件で動く）。
