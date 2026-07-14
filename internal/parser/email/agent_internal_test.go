package email

import "testing"

// TestFirstHTTPSTrimsJapanesePunctuation は、空白で切り出した URL の末尾に
// 和文約物が貼り付いてもリンクが壊れないことを固定する。
//
// 空白・タブ・全角空白でしか切らないと「（https://…）」「https://…。」で
// 閉じ括弧や句読点が URL へ混ざる。
func TestFirstHTTPSTrimsJapanesePunctuation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"全角括弧に挟まれた URL", "（https://example.test/x）", "https://example.test/x"},
		{"末尾の句点を落とす", "https://example.test/y。", "https://example.test/y"},
		{"複数の約物が続いても落とす", "https://example.test/z）。」", "https://example.test/z"},
		{"約物が無ければそのまま", "https://example.test/clean", "https://example.test/clean"},
		{"空白があれば従来どおり空白で切る", "詳細は https://example.test/w をご確認ください", "https://example.test/w"},
		{"https が無ければ空", "案件の詳細はメール本文をご覧ください", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := firstHTTPS(tt.in); got != tt.want {
				t.Errorf("firstHTTPS(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
