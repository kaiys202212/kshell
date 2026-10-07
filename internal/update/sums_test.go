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
	_, err := ParseSHA256SUMS([]byte("deadbeef  other.zip\n"), ZipName)
	if err == nil {
		t.Fatal("缺目标文件应失败")
	}
	if want := "err.update.sums_missing_file|" + ZipName; err.Error() != want {
		t.Fatalf("err = %q, want %q", err.Error(), want)
	}
}
