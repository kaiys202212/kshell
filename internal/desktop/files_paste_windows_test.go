//go:build windows

package desktop

import "testing"

func TestParseDropEffect(t *testing.T) {
	cases := []struct {
		name string
		b    []byte
		want bool
	}{
		{"复制 DROPEFFECT_COPY", []byte{1, 0, 0, 0}, false},
		{"剪切 DROPEFFECT_MOVE", []byte{2, 0, 0, 0}, true},
		{"复制|链接", []byte{5, 0, 0, 0}, false},
		{"移动|复制", []byte{3, 0, 0, 0}, true},
		{"空", nil, false},
		{"不足 4 字节", []byte{2, 0}, false},
	}
	for _, c := range cases {
		if got := parseDropEffect(c.b); got != c.want {
			t.Fatalf("%s: parseDropEffect(%v) = %v, want %v", c.name, c.b, got, c.want)
		}
	}
}
