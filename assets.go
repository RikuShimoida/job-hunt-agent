// Package assets は init が生成する設定ひな形をバイナリへ埋め込む。
//
// 埋め込み指示子は .. を辿れずパッケージ配下しか参照できない。ひな形は配置が
// リポジトリ直下（.env.example）と config/（*.example.yaml）にまたがるため、両方を
// 1 パッケージから見られるのはリポジトリ直下だけ。テストと README がディスク上の
// example を参照し続けるので、ファイルは動かさず現位置のまま埋め込む
// （migrations が root 直下でパッケージ化して *.sql を埋め込むのと同じ置き方）。
package assets

import "embed"

//go:embed .env.example config/profile.example.yaml config/sources.example.yaml
var FS embed.FS
