package desktop

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
)

const (
	biRGB       = 0
	biBitfields = 3
)

// dibToPNG 把 CF_DIB（BITMAPINFO + 像素）转成 PNG。只支持常见的 24/32 位无压缩截图。
func dibToPNG(dib []byte) ([]byte, error) {
	if len(dib) < 40 {
		return nil, fmt.Errorf("err.clipboard.dib_too_short")
	}
	headerSize := int(binary.LittleEndian.Uint32(dib[0:4]))
	if headerSize < 40 || headerSize > len(dib) {
		return nil, fmt.Errorf("err.clipboard.dib_header_invalid")
	}
	width := int(int32(binary.LittleEndian.Uint32(dib[4:8])))
	heightSigned := int32(binary.LittleEndian.Uint32(dib[8:12]))
	topDown := heightSigned < 0
	height := int(heightSigned)
	if height < 0 {
		height = -height
	}
	if width <= 0 || height <= 0 || width > 16384 || height > 16384 {
		return nil, fmt.Errorf("err.clipboard.dib_size_invalid")
	}
	bitCount := binary.LittleEndian.Uint16(dib[14:16])
	compression := binary.LittleEndian.Uint32(dib[16:20])
	if bitCount != 24 && bitCount != 32 {
		return nil, fmt.Errorf("err.clipboard.dib_bit_depth|%d", bitCount)
	}
	if compression != biRGB && compression != biBitfields {
		return nil, fmt.Errorf("err.clipboard.dib_compression|%d", compression)
	}

	pixelOff := headerSize
	if compression == biBitfields && headerSize == 40 {
		pixelOff += 12
	}
	if pixelOff > len(dib) {
		return nil, fmt.Errorf("err.clipboard.dib_pixel_offset_invalid")
	}

	rowBytes := ((width*int(bitCount) + 31) / 32) * 4
	need := pixelOff + rowBytes*height
	if need > len(dib) {
		return nil, fmt.Errorf("err.clipboard.dib_insufficient_pixels")
	}

	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		srcY := y
		if !topDown {
			srcY = height - 1 - y
		}
		row := dib[pixelOff+srcY*rowBytes:]
		for x := 0; x < width; x++ {
			switch bitCount {
			case 32:
				i := x * 4
				dst.SetNRGBA(x, y, color.NRGBA{R: row[i+2], G: row[i+1], B: row[i], A: 255})
			case 24:
				i := x * 3
				dst.SetNRGBA(x, y, color.NRGBA{R: row[i+2], G: row[i+1], B: row[i], A: 255})
			}
		}
	}
	return encodePNG(dst)
}
