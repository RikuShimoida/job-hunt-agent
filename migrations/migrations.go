// Package migrations はスキーマ定義の SQL をバイナリへ埋め込む。
//
// 埋め込み指示子はパッケージ配下しか参照できないため、SQL を置いたこのディレクトリ
// 自体をパッケージにしている。SQL を internal/ 配下へ移すと「スキーマがどこにあるか」
// が分かりにくくなるため、リポジトリ直下の migrations/ という置き場所を優先した。
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
