---
name: git-problem
description: git statusなどgitコマンドが重い問題を診断。.gitignore、追跡ファイル、リポジトリサイズ、Git設定をチェック。
when_to_use: 「gitが重い」「git statusが遅い」「gitコマンドが遅い」「git-problem」「gitが固まる」「コミットが遅い」などの発言時
context: fork
agent: dev-doctor
---

## Gitパフォーマンス問題の診断フロー

引数: `$ARGUMENTS`（省略可。特定の症状があれば記載）

### Phase 1: リポジトリサイズ確認

1. **`.git`ディレクトリサイズ**: `du -sh .git`
2. **オブジェクト統計**: `git count-objects -vH`
3. **追跡ファイル数**: `git ls-files | wc -l`
4. **未追跡ファイル数**: `git ls-files --others --exclude-standard | wc -l`

### Phase 2: .gitignore整合性チェック

以下がgitに追跡されていないか確認する:

1. **worktree**: `git ls-files --cached .claude/worktrees 2>/dev/null | head -20`
2. **ビルド成果物・バイナリ**: `git ls-files --cached bin dist 2>/dev/null | head -20`
3. **vendor**（vendoring していない場合）: `git ls-files --cached vendor 2>/dev/null | head -5`
4. **カバレッジ・プロファイル**: `git ls-files --cached '*.out' '*.prof' coverage.txt 2>/dev/null | head -20`
5. **`.env`**: `git ls-files --cached '.env*' 2>/dev/null | head -10`
   — **追跡されていたら High として即座に報告する**（機密が履歴に残っている可能性）
6. **.gitignoreの内容確認**: 現在の `.gitignore` に必要なパターンが含まれているか検証

追跡されているべきでないファイルが見つかった場合は、`.gitignore` の修正と `git rm --cached` を提案する。

### Phase 3: 巨大ファイル検出

1. **追跡中の巨大ファイル（1MB超）**:
   ```
   git ls-files -z | xargs -0 -I{} sh -c 'size=$(wc -c < "{}"); if [ "$size" -gt 1048576 ]; then echo "${size} {}"; fi' | sort -rn | head -10
   ```
2. **履歴内の大きなblob（1MB超）**:
   ```
   git rev-list --objects --all | git cat-file --batch-check='%(objecttype) %(objectname) %(objectsize) %(rest)' | awk '$1 == "blob" && $3 > 1048576 {print $3, $4}' | sort -rn | head -10
   ```

Go ではコンパイル済みバイナリ（数十MB）を誤ってコミットしがちなので特に注意する。

### Phase 4: Git設定・パフォーマンス最適化

1. **fsmonitor設定**: `git config core.fsmonitor`
2. **preloadindex設定**: `git config core.preloadindex`
3. **untrackedCache設定**: `git config core.untrackedCache`
4. **Git GC状態**: `git gc --auto --dry-run 2>&1`

### 診断結果の報告

```
## 診断結果

### リポジトリサマリ
- .gitサイズ: X MB
- 追跡ファイル数: X
- 未追跡ファイル数: X
- オブジェクト数: X

### 検出された問題
1. **[High/Medium/Low]** 問題の説明
   - 原因: ...
   - 修復コマンド: `...`

### 推奨アクション（優先順位順）
1. `修復コマンド` — 説明
```

### 修復実行のルール

- 非破壊的操作は承認なしで実行可:
  - `git config core.fsmonitor true`
  - `git config core.untrackedCache true`
  - `git config core.preloadindex true`
- 破壊的操作は目的・影響範囲・リスクを明示し、ユーザーの承認後に実行:
  - 追跡解除: `git rm --cached <path>`（コミット履歴に残るため慎重に）
  - ガベージコレクション: `git gc --prune=now`
  - `.gitignore` への追記（変更内容を提示し承認後に実行）
- 修復後は再度チェックを実行し、問題が解消されたことを確認する

### 禁止事項

- `git push --force` の実行
- `git reset --hard` の実行
- `git rebase` の実行
- ユーザー承認なしでの `git rm --cached` の実行
- `run_in_background: true` の使用
