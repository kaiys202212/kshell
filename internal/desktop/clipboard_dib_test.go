package desktop

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"testing"
)

func TestDibToPNG_1x1Red(t *testing.T) {
	dib := make([]byte, 40+4)
	binary.LittleEndian.PutUint32(dib[0:4], 40)
	binary.LittleEndian.PutUint32(dib[4:8], 1)
	binary.LittleEndian.PutUint32(dib[8:12], 1)
	binary.LittleEndian.PutUint16(dib[12:14], 1)
	binary.LittleEndian.PutUint16(dib[14:16], 32)
	dib[40], dib[41], dib[42], dib[43] = 16, 32, 200, 0 // BGRA → 近似红

	pngBytes, err := dibToPNG(dib)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := img.At(0, 0).RGBA()
	if a>>8 != 255 || r>>8 != 200 || g>>8 != 32 || b>>8 != 16 {
		t.Fatalf("pixel rgba=%d,%d,%d,%d", r>>8, g>>8, b>>8, a>>8)
	}
}
