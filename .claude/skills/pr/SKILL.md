---
name: pr
description: ブランチ・Git運用ルール。feature ブランチ作成、コミット、プッシュ、PR作成の手順。品質チェック後のGit操作フロー。
when_to_use: PR作成時、ブランチ作成時、「PR作って」「プルリク」「pushして」「PRお願い」などの発言時
---

## ブランチ・Git 運用ルール

### ブランチ作成

- 修正前に必ず develop ブランチから feature ブランチを作成する
- 命名規則: `feature/<issue番号>-<英語スラッグ>` / `bugfix/<issue番号>-<英語スラッグ>`
  - 例: `feature/12-add-resume-parser`

### コミット〜PR作成（順序厳守）

品質チェック（format → vet → lint → test）が完了していることを前提に、以下を実行する。

1. **`git status` を確認し、差分が存在することを確認する**
2. **すべての差分を含めてコミットする**
   - フォーマット・lint 修正による差分を含めること
   - 依存を追加したなら `go.mod` / `go.sum` の差分も含めること
   - コミットメッセージは日本語。Issue 番号を含める
3. **プッシュする**
4. **PR を作成する（into は develop）**
   - **PR 名および本文は必ず日本語で記載する**

### 完了条件

- 作業完了時点で `git status` が clean であること
- コミット後に新たな差分を発生させ、そのまま放置しないこと

### PR と Issue のクローズルール

- **PR をクローズ（マージまたは Close）した場合、関連付けられている Issue も必ずクローズすること**
- PR と Issue の紐付けは、PR 作成時に本文に `Closes #issue番号` を記載することで行う。
  - **ただし本リポジトリのPRはベースが `develop`（非デフォルトブランチ）のため、`Closes #N` を書いても
    GitHub による自動クローズは働かない**（自動クローズはデフォルトブランチ `main` 向けPRのみ）。
  - 通常のマージ経路では `/merge` スキルがマージ後に PR 本文の `Closes #N` を抽出し `gh issue close` でクローズする。
- 手動で PR を Close した場合は、必ず関連 Issue も手動で Close すること
- Issue 番号の確認方法:
  - PR 作成時に紐付けた Issue 番号を確認
  - `gh pr view <PR番号>` で関連 Issue を確認

### 実行コマンド例

```bash
# 通常はマージを /merge スキルに任せる（CI 見届け + マージ + ブランチ削除 + Issue クローズまで一貫実行）。
# 以下は /merge を使わず手動でマージする場合の例。
# develop ベースのため "Closes #123" による自動クローズは働かないので、Issue は明示的にクローズする。
gh pr merge <PR番号> --merge
gh issue close <Issue番号>

# 手動で PR をクローズ（マージせず Close）した場合も、Issue を手動でクローズ
gh pr close <PR番号>
gh issue close <Issue番号>
```
