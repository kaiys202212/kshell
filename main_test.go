package main

import "testing"

// isExecutableMagic 覆盖三类可执行头与占位文件（README 等）的排除。
func TestIsExecutableMagic(t *testing.T) {
	cases := map[[4]byte]bool{
		{'M', 'Z', 0x00, 0x00}:   true,  // PE
		{0x7f, 'E', 'L', 'F'}:    true,  // ELF
		{0xCF, 0xFA, 0xED, 0xFE}: true,  // Mach-O 64
		{0xCA, 0xFE, 0xBA, 0xBE}: true,  // Mach-O 32
		{'#', ' ', 'b', 'u'}:     false, // Markdown 占位
		{0x00, 0x00, 0x00, 0x00}: false, // 全零
		{'P', 'K', 0x03, 0x04}:   false, // zip
	}
	for in, want := range cases {
		if got := isExecutableMagic(in[:]); got != want {
			t.Fatalf("isExecutableMagic(% x) = %v, want %v", in, got, want)
		}
	}
}
