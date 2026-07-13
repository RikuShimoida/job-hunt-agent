---
name: golangci-lint-stale-worktree-cache
description: golangci-lint が削除済み worktree の絶対パスをキャッシュし、実在しないファイルの偽の指摘を出すことがある
metadata:
  type: project
---

worktree を撤去した後、別の worktree で `golangci-lint run` を実行すると、
**削除済み worktree の絶対パスを指す偽の指摘**が出ることがある。

実例: `reviewfix-8` の worktree で lint すると、既に撤去済みの
`.claude/worktrees/feature-7-scaffold-mvp-pipeline/internal/.../repository.go:33` に対する
errcheck 指摘が1件出た。そのファイルは存在せず、`failed to parse file: ... no such file or directory`
の warning も併記されていた。

**Why:** 1タスク=1 worktree の運用（@docs/worktree-workflow.md）で worktree の絶対パスが
頻繁に生まれては消えるため、golangci-lint / go build のキャッシュが古いパスを保持し続ける。

**How to apply:** lint の指摘が「実在しないパス」を指していたら、コードの問題ではなくキャッシュを疑う。
`golangci-lint cache clean` と `go clean -cache` を実行して再実行すれば解消する
（実際にこれで 1 issue → 0 issues になった）。指摘を無理に修正しようとしないこと。
