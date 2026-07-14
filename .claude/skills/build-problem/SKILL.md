---
name: build-problem
description: ビルドが通らない・コンパイルエラー・モジュール依存が解決できない問題を診断。ツールチェーン、go.mod/go.sum、依存の衝突、ビルドキャッシュをチェックする。
when_to_use: 「ビルドが通らない」「コンパイルエラー」「go mod エラー」「依存が解決できない」「build-problem」「型が合わない」などの発言時
context: fork
agent: dev-doctor
---

## ビルド・依存問題の診断フロー

引数: `$ARGUMENTS`（省略可。エラーメッセージや対象パッケージがあれば記載）

### Phase 1: ツールチェーンの確認

1. **Go バージョン**: `go version`
2. **`go.mod` の go ディレクティブ**: `grep '^go ' go.mod`（要求バージョンとインストール済みが一致するか）
3. **環境変数**: `go env GOMODCACHE GOTOOLCHAIN GOFLAGS GOPRIVATE`
4. **golangci-lint**: `golangci-lint --version`

インストール済み Go が `go.mod` の要求より古い場合、それが直接の原因になり得る。

### Phase 2: モジュール依存の整合性

1. **依存の検証**: `go mod verify`（`all modules verified` が正常）
2. **tidy 差分**: `go mod tidy -diff 2>&1`
   - 差分が出る = `go mod tidy` が未実行。`go.sum` の不足や不要な依存が残っている
3. **依存グラフ**: `go list -m all 2>&1 | head -30`
4. **特定依存がなぜ必要か**: `go mod why <module>`
5. **バージョン衝突の確認**: `go list -m -u all 2>&1 | grep -i conflict`

### Phase 3: コンパイルエラーの特定

1. **ビルド**: `go build ./... 2>&1 | head -50`
2. **静的解析**: `go vet ./... 2>&1 | head -50`
3. **型チェックのみ（高速）**: `go build -o /dev/null ./... 2>&1 | head -50`

エラーメッセージは**最初のエラーから読む**。Go のコンパイルエラーは連鎖するため、
2番目以降のエラーは1番目の派生であることが多い。

### Phase 4: キャッシュ状態

1. **ビルドキャッシュのサイズ**: `du -sh "$(go env GOCACHE)" 2>/dev/null`
2. **モジュールキャッシュのサイズ**: `du -sh "$(go env GOMODCACHE)" 2>/dev/null`

> キャッシュ破損は稀。**キャッシュ削除は最後の手段**であり、原因を特定せずに提案してはならない。

### 診断結果の報告

```
## 診断結果

### 環境サマリ
- Go バージョン: X（go.mod 要求: Y）
- モジュール整合性: OK / NG
- go mod tidy 差分: なし / あり
- ビルド: 通る / 通らない

### 検出された問題
1. **[High/Medium/Low]** 問題の説明
   - 原因: ...
   - 修復コマンド: `...`

### 推奨アクション（優先順位順）
1. `修復コマンド` — 説明
```

### 修復実行のルール

- 非破壊的操作は承認なしで実行可:
  - `go mod tidy`
  - `go mod download`
  - `go build ./...` / `go vet ./...`
  - `gofmt -w .`
- 破壊的操作は目的・影響範囲・リスクを明示し、ユーザーの承認後に実行:
  - `go clean -cache`（ビルドキャッシュ削除。次回ビルドが大幅に遅くなる）
  - `go clean -modcache`（モジュールキャッシュ全削除。全依存の再ダウンロードが必要）
  - 依存のバージョン変更: `go get <module>@<version>`
  - `go.mod` の go ディレクティブ変更
- 修復後は再度チェックを実行し、問題が解消されたことを確認する

### 禁止事項

- `run_in_background: true` の使用
- ユーザー承認なしでの破壊的操作の実行
- 原因を特定せずにキャッシュ削除を提案すること
- コンパイルエラーを「とりあえず型アサーションで黙らせる」修正を提案すること
