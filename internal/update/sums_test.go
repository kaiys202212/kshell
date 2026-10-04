package update

import "testing"

func TestParseSHA256SUMS(t *testing.T) {
	const sum = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	data := []byte(sum + "  " + ZipName + "\n")
	got, err := ParseSHA256SUMS(data, ZipName)
	if err != nil {
		t.Fatal(err)
	}
	if got != sum {
		t.Fatalf("got %q", got)
	}
}

func TestParseSHA256SUMSMissing(t *testing.T) {
	if _, err := ParseSHA256SUMS([]byte("deadbeef  other.zip\n"), ZipName); err == nil {
		t.Fatal("缺目标文件应失败")
	}
}
