---
name: impl
description: Issue起点の実装フロー。ブランチ作成→実装→テスト→品質チェック→コミット→PR作成→完了報告を一貫して実行する。事前に /plan で計画承認済みであることが前提。
when_to_use: 「実装して」「このIssueやって」「implement」などの発言時、Issue番号を指定して実装を依頼された時。事前に /plan で計画がユーザーに承認されていること。
agent: go-engineer
---

## Issue起点の実装

引数: `$ARGUMENTS`（Issue番号。例: 123）

### 前提条件

- `/plan $ARGUMENTS` で計画を出力し、ユーザーが承認済みであること
- 計画未承認の場合は、先に `/plan $ARGUMENTS` を実行するよう案内して終了すること
- **このスキルは worktree 起点の常時運用が前提**。設計の全体像は `docs/worktree-workflow.md` を参照。
  単独実装でも割り込み実装でも、1タスク = 1 worktree で行い、develop メインは司令塔として clean に保つ。

#### worktree モードと撤退路（`--no-worktree`）

- **既定は worktree モード**（1タスク = 1 worktree）。
- 引数に **`--no-worktree`** が含まれる場合は、worktree を作らず**従来の checkout 方式**で実装する
  （1-0b / 1-1b を参照）。「今回は worktree が割に合わない」と判断したタスクで、その場で切り替えられる。
  - 例: `/impl 21 --no-worktree`

### Phase 1: 実装

> 以下 1-0 / 1-1 は worktree モードの手順。`--no-worktree` 指定時は代わりに 1-0b / 1-1b を行う。

#### 1-0. 前提チェック（worktree モード。満たさなければ停止）

- `git branch --show-current` が `develop` であること。
  - develop メイン会話を司令塔とし、ここから worktree を切る運用のため。
  - develop 以外なら「`git switch develop` してから再実行してください」と案内して停止。
- 司令塔（develop メイン）の作業ツリーが clean であること（`git status --porcelain` が空）。
  - clean でなければ、未コミット変更の扱いをユーザーに確認してから進む。
- **司令塔のローカル `develop` を最新化する**（clean を確認した後に実行）:
  ```
  git pull --ff-only origin develop
  ```
  - **なぜ必須か**: worktree 自体は 1-1 の `git worktree add ... origin/develop` で常に最新起点になるが、
    司令塔のローカル `develop` が古いままだと、`git status` の見え方・差分の基準・PR後の後続作業がリモートと
    ズレる。司令塔は指揮所なので、常にリモート `develop` に追従させておく。
  - `--ff-only` が失敗する場合（ローカル `develop` に余分なコミットがある等）は、勝手に rebase / merge せず、
    状況をユーザーに報告して指示を仰ぐ。

#### 1-0b. 前提チェック（`--no-worktree` 時）

- 既存の checkout 方式の作法に従う。worktree は作らない。
- 並列実装はできない（メインの作業ツリーを1つ占有するため）。単独タスク向け。

#### 1-1. worktree とブランチの作成（worktree モード）

- **このタスク専用の worktree を develop 起点で作成する**（1タスク = 1 worktree）。
- ブランチ命名規則: `feature/<issue番号>-<英語スラッグ>` または `bugfix/<issue番号>-<英語スラッグ>`
  - 例: `feature/21-add-resume-parser`, `bugfix/34-fix-token-refresh`
- 手順:
  ```
  git fetch origin develop
  git worktree add .claude/worktrees/<prefix>-$ARGUMENTS-<英語スラッグ> origin/develop -b <prefix>/$ARGUMENTS-<英語スラッグ>
  ```
  - **なぜ生の `git worktree add` か**: Claude Code の Agent `isolation: "worktree"` は既定ベースが
    `origin/HEAD`(=main) で develop 起点に固定できない。ベースを明示できる生コマンドを使う
    （`docs/worktree-workflow.md` §4 参照）。
- 以降の実装作業（1-2〜1-6）は、すべてこの worktree ディレクトリ内で行う。

##### worktree の初期化

Go はモジュールキャッシュ（`GOMODCACHE`）をマシン全体で共有するため、**worktree ごとの依存インストールは不要**。
新 worktree でやることは以下だけ:

1. 環境ファイル（`.env` 等）が存在する場合のみ、新 worktree へコピーする。
2. 必要なら `go build ./...` で初回ビルドキャッシュを温める（省略可）。

> これが BeerSalon（pnpm install が毎回必要）との最大の差であり、Go では worktree の
> 固定オーバーヘッドがほぼゼロになる。常時 worktree 運用が素直に成立する理由でもある。

#### 1-1b. ブランチ作成（`--no-worktree` 時）

```
git switch develop
git pull origin develop
git switch -c <prefix>/$ARGUMENTS-<英語スラッグ>
```

##### 英語スラッグの決め方

- Issue内容から Claude 自身が短い英語スラッグを決める
- ルール:
  - 小文字 + ハイフン区切り（ケバブケース）
  - 日本語禁止
  - 3〜5語以内
  - Issueタイトルの直訳ではなく、要点を短く表現する
    - 例: 「職務経歴書のパース機能を追加する」→ `add-resume-parser`
    - 例: 「トークンリフレッシュが失敗するバグ修正」→ `fix-token-refresh`
- プレフィックスの選択:
  - バグ修正系Issue → `bugfix/`
  - それ以外（機能追加・改善・リファクタ等）→ `feature/`

##### Issue とブランチの紐付け

- PR本文に `Closes #$ARGUMENTS` を記載することで Issue と紐付ける（Phase 2-2）。
- **注意**: 本リポジトリのPRはベースが `develop`（非デフォルトブランチ）のため、`Closes #N` を本文に書いても
  GitHub による Issue 自動クローズは**働かない**（自動クローズはデフォルトブランチ `main` 向けPRのみ）。
  `Closes #N` は紐付け表示・`/merge` がマージ後に Issue 番号を抽出する根拠として記載する。
  実際のクローズはマージ時に `/merge` スキルが `gh issue close` で行う。

#### 1-2. プロダクトコード実装

- Issue の要件に基づきコードを実装する
- `.claude/rules/go.md` の規約に従う

#### 1-3. UT実装・実行

- テーブル駆動のユニットテストを実装する
- `go test ./...` で全テストがパスすることを確認する
- 並行処理に触れた変更なら `go test -race ./...` も実行する
- UT は外部依存をフェイクに差し替えるため、複数 worktree で同時に実行してよい（並列OK）

#### 1-3-1. 統合テスト（IT）の扱い

- 外部サービス・実DB・実ファイルに接続する検証は `//go:build integration` タグで分離し、
  `go test -tags=integration ./...` で実行する。
- **現時点では共有ランタイム基盤（DB・固定ポート等）を持たないため、IT も worktree 内で実行してよい。**
  将来 DB や固定ポートを使う外部依存を導入した場合は、`docs/worktree-workflow.md` §2 の
  「共有リソース制約」に従い、司令塔での直列消化に切り替えること。

#### 1-4. 受入条件の検証

- Issue の受入条件を、テストまたは実際の実行（`go run ./cmd/...` 等）で満たすことを確認する。
- **検証できないまま作業完了としてはならない。**
- 不具合を発見した場合、Issue のスコープ外なら修正せず、再現手順・期待結果・実際の結果を報告する。

#### 1-5. 品質チェック（順序厳守）

1. `gofmt -w .`
2. `go vet ./...`
3. `golangci-lint run`（エラーがあれば修正し、再度 gofmt を実行）
4. `go test ./...`
5. 依存を追加した場合は `go mod tidy`

#### 1-6. 設計ドキュメント同期

- 実装内容に応じて README.md / docs/architecture.md を更新する
- コードと設計ドキュメントの内容が一致していることを確認する

### Phase 2: 完了

#### 2-1. コミット・プッシュ

- すべての変更（コード・テスト・ドキュメント・`go.mod`/`go.sum`）をコミットする
- コミットメッセージにIssue番号を含める（日本語）
- プッシュする

#### 2-2. PR作成

- `gh pr create --base develop` でPRを作成する
- **PR名・本文は日本語で記載する**
- PR本文に `Closes #Issue番号` を記載する（Issue との紐付け用。develop ベースのため自動クローズは働かず、
  実際のクローズはマージ時に `/merge` スキルが行う。1-1 の「Issue とブランチの紐付け」参照）

#### 2-2-1. worktree の後片付け

- **worktree モードの場合のみ実施**（`--no-worktree` 時はスキップ）。
- PR作成が完了したら、このタスクの worktree を撤去する:
  ```
  git worktree remove .claude/worktrees/<このタスクのworktree>
  ```
- 未コミット変更が残っている場合は撤去せず、その旨を完了報告に明記する。

#### 2-2-2. 司令塔フェーズ（コードレビュー）

PR作成後、品質ゲートは worktree 内ではなく**司令塔（develop メイン）が集約して実施する**。
worktree は既に 2-2-1 で撤去済みのため、以降はすべて develop メインの作業ツリーから行う。

1. **コードレビュー（`pr-review` スキルへの委譲）**
   - 司令塔が **`pr-review` スキルを 2-2 で作成した PR番号付きで起動する**（`/pr-review <PR番号>` 相当）。
     - `pr-review` は `agent: system-architect`・`context: fork` で動く。
       エージェント一貫性ルールの例外として、レビューに限り system-architect への委譲を認める（後述）。
   - **レビュー観点・コメントフォーマットの正は `pr-review` スキル（`.claude/skills/pr-review/SKILL.md`）が単一の真実の源**。
     impl 側に観点やフォーマットを複製しない（二重メンテ防止）。
   - **結果は PR コメントとして投稿するのみ**。指摘の取り込み可否はユーザーが判断する
     （このフロー内でコード修正は行わない）。

   - **pr-review 完了後の分岐（必須）**: pr-review が PR にレビューコメントを投稿し終えたら、
     司令塔に戻った時点で「GitHub にレビュー指摘の内容をコメントしました」と報告し、
     **AskUserQuestion で次の3択をユーザーに必ず確認する**:
     1. **このまま `/review-fix <PR番号>` を実行する** — 指摘をコード修正で取り込む
     2. **このままマージする** — `/merge <PR番号>` スキルの手順を実行（Merge commit + リモート/ローカルのブランチ削除 + 司令塔develop最新化）
     3. **あとで決める** — 何もせず元の会話に戻る。マージしたくなったら後から `/merge <PR番号>` を叩けばよい
     - **3択を出さずに勝手にマージ・review-fix してはならない**。マージのタイミングはユーザーが決める。
     - 「あとで決める」を選んだ場合、PR番号を完了報告（2-3）に明記し、`/merge <PR番号>` で後追いできる旨を添える。

2. **共有リソース依存テストが将来導入された場合**
   - 現時点では該当なし（1-3-1 参照）。
   - DB や固定ポートを使う IT を導入した後は、司令塔が `qa-engineer` を1体ずつ直列起動して消化する
     （`docs/worktree-workflow.md` §3）。

#### 2-3. 完了報告

以下を報告する:

1. 修正内容（何を変更したか）
2. 修正理由（なぜその変更が必要だったか）
3. 変更ファイル一覧
4. UT結果（`go test ./...` の結果。`-race` を回したならその結果も）
5. IT結果（実施した場合。未実施なら理由を明記）
6. 受入条件をどのように検証したか
7. コードレビュー結果（PRコメントURL・指摘件数と重要度の内訳）。指摘への対応（`/review-fix` 実行など）はユーザー判断に委ねる旨も添える。
   2-2-2 の3択で「あとで決める」を選んだ場合は、後から `/merge <PR番号>` でマージできる旨と対象PR番号を明記する
8. 設計ドキュメント更新の有無と内容
9. 破壊的変更の有無と内容（該当する場合のみ）
10. PR URL

**破壊的変更の報告義務**: 実装中に以下のような破壊的変更を行った場合は、完了報告の項目9で必ず明示すること。

- 公開関数・公開型のシグネチャ変更、公開識別子の削除・リネーム
- CLI フラグ・サブコマンド・環境変数の追加・変更・削除
- API エンドポイントの入出力契約の変更
- データスキーマの変更（マイグレーションを要する変更）
- 依存パッケージの追加・削除・メジャーバージョン変更
- Go のバージョン要件変更（`go.mod` の `go` ディレクティブ）

報告時は「何を変更したか」「なぜ必要だったか」「ユーザー側で必要な対応」の3点を含めること。

#### 2-4. 実装理解チェック

完了報告の直後に `/understand` スキルの手順を実行する。

**この自動発動では、モード選択（「いま解く」/「宿題にする」）は聞かれず、常に宿題化される**。
実装に充てたい時間を理解チェックの対話に取られないようにするため、`/understand` の自動発動（impl 経由）は
Phase M（モード選択）を経由せず、直接 Phase H（宿題化）へ直行する設計になっている
（`.claude/skills/understand/SKILL.md` の起動分岐を参照）。

- `/understand` は今回の問いを**宿題用 Issue として起票する**（ローカルファイルは作らない）。
- 起票が完了した時点でこのスキルは終了する（解き終えるまでブロックしない）。
  Issue 起票に失敗した場合も、その旨を報告して完了とみなして終了してよい（impl を止めない）。

### エージェント一貫性の強制ルール

このスキルは `agent: go-engineer` で実行される。以下を厳守すること。

- **go-engineer が Phase 1（実装）から Phase 2（完了）まで一貫して全フェーズを担当すること**
- Phase 間でエージェントを切り替えたり、汎用エージェントに委譲してはならない
- サブエージェントを起動する場合も、必ず `subagent_type: "go-engineer"` を指定すること
- `subagent_type` 未指定（汎用 Agent）でのサブエージェント起動は禁止
- **例外（2-2-2 司令塔フェーズのみ）**: 実装者と異なる視点を確保するため、
  コードレビューは `system-architect`、共有リソース依存テストは `qa-engineer` への委譲を認める。
  これらは実装そのものではなく品質ゲートであり、go-engineer の自己レビュー/自己テストでは
  独立性が担保できないため、別エージェントに分担させる。実装作業（Phase 1）への委譲例外ではない。

### 禁止事項

- 受入条件の検証完了前にコミット・プッシュ・PR作成すること
- 品質チェック（format / vet / lint / test）の修正差分をコミットに含めず放置すること
- `run_in_background: true` の使用
