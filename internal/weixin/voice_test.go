package weixin

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	silk "github.com/yoshino-s/silk-go"
)

// encodeSilk 用 silk-go 编码 PCM 生成测试用 SILK 流（tencent 兼容模式带 STX 前缀）。
func encodeSilk(t *testing.T, pcm []byte, tencent bool) []byte {
	t.Helper()
	out, err := silk.EncodePCM(pcm, silk.EncodeOptions{
		SampleRateHz:  24000,
		PacketSizeMs:  20,
		TencentCompat: map[bool]int32{false: 0, true: 1}[tencent],
	})
	if err != nil {
		t.Fatalf("SILK 编码: %v", err)
	}
	return out
}

// assertWAV 校验 WAV 头（16-bit 单声道 sampleRate）并返回 PCM 载荷。
func assertWAV(t *testing.T, wav []byte, sampleRate int) []byte {
	t.Helper()
	if len(wav) <= 44 || !bytes.HasPrefix(wav, []byte("RIFF")) || !bytes.HasPrefix(wav[8:], []byte("WAVE")) {
		t.Fatalf("WAV 头非法: len=%d", len(wav))
	}
	if got := binary.LittleEndian.Uint16(wav[20:]); got != 1 {
		t.Errorf("音频格式 = %d, 期望 PCM(1)", got)
	}
	if got := binary.LittleEndian.Uint16(wav[22:]); got != 1 {
		t.Errorf("声道数 = %d, 期望 1", got)
	}
	if got := int(binary.LittleEndian.Uint32(wav[24:])); got != sampleRate {
		t.Errorf("采样率 = %d, 期望 %d", got, sampleRate)
	}
	if got := binary.LittleEndian.Uint16(wav[34:]); got != 16 {
		t.Errorf("位深 = %d, 期望 16", got)
	}
	dataLen := binary.LittleEndian.Uint32(wav[40:])
	if int(dataLen) != len(wav)-44 {
		t.Errorf("data 长度 = %d, 期望 %d", dataLen, len(wav)-44)
	}
	return wav[44:]
}

func TestSilkToWAVTencentHeader(t *testing.T) {
	// 微信风格：STX(0x02) + #!SILK_V3 头（TencentCompat 编码产物）。
	pcm := bytes.Repeat([]byte{0x10, 0x02}, 2400) // 48KB 近似正弦平段，20ms 帧可编码
	silkData := encodeSilk(t, pcm, true)
	wav, err := silkToWAV(silkData)
	if err != nil {
		t.Fatalf("silkToWAV: %v", err)
	}
	got := assertWAV(t, wav, 24000)
	if len(got) == 0 {
		t.Fatal("PCM 载荷为空")
	}
}

func TestSilkToWAVPlainHeader(t *testing.T) {
	// 标准头（无 STX 前缀）同样可解。
	pcm := bytes.Repeat([]byte{0x00, 0x10}, 2400)
	silkData := encodeSilk(t, pcm, false)
	wav, err := silkToWAV(silkData)
	if err != nil {
		t.Fatalf("silkToWAV: %v", err)
	}
	assertWAV(t, wav, 24000)
}

func TestSilkToWAVInvalid(t *testing.T) {
	if _, err := silkToWAV([]byte("这不是 SILK 数据")); err == nil {
		t.Error("垃圾数据应报错")
	}
	if _, err := silkToWAV(nil); err == nil {
		t.Error("空数据应报错")
	}
}

// TestDetectAudioFormat 覆盖解码前嗅探：silkcc 只认 SILK v3，其余格式必须
// 在解码前识别出来，供回执与日志给出可定位信息。
func TestDetectAudioFormat(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"腾讯兼容 SILK", append([]byte{0x02}, []byte("#!SILK_V3")...), audioFormatSilk},
		{"标准 SILK", []byte("#!SILK_V3\x00\x00"), audioFormatSilk},
		{"AMR", []byte("#!AMR\n\x00\x00"), audioFormatAMR},
		{"AMR-WB", []byte("#!AMR-WB\n\x00\x00"), audioFormatAMR},
		{"MP3 ID3", []byte("ID3\x04\x00\x00"), audioFormatMP3},
		{"MP3 帧同步", []byte{0xFF, 0xFB, 0x90, 0x64}, audioFormatMP3},
		{"M4A ftyp", []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '}, audioFormatM4A},
		{"OGG", []byte("OggS\x00\x02"), audioFormatOGG},
		{"WAV", append([]byte("RIFF\x00\x00\x00\x00"), []byte("WAVEfmt ")...), audioFormatWAV},
		{"未知", []byte{0x00, 0x01, 0x02, 0x03}, audioFormatUnknown},
		{"空", nil, audioFormatUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectAudioFormat(tc.data); got != tc.want {
				t.Fatalf("detectAudioFormat = %q, 期望 %q", got, tc.want)
			}
		})
	}
}

// TestSilkToWAVNonSilkIsTyped 非 SILK 载荷必须返回可归因错误，携带实际格式
// 与首部字节，而不是原始的 silkcc -1000。
func TestSilkToWAVNonSilkIsTyped(t *testing.T) {
	amr := []byte("#!AMR\n\x01\x02\x03\x04")
	_, err := silkToWAV(amr)
	if err == nil {
		t.Fatal("非 SILK 载荷应报错")
	}
	var uf *UnsupportedVoiceFormatError
	if !errors.As(err, &uf) {
		t.Fatalf("错误应为 *UnsupportedVoiceFormatError, 实际 %T: %v", err, err)
	}
	if uf.Format != audioFormatAMR {
		t.Errorf("格式 = %q, 期望 %q", uf.Format, audioFormatAMR)
	}
	if !bytes.HasPrefix(uf.Head, amr[:8]) {
		t.Errorf("首部字节应保留原始前 8 字节: %X", uf.Head)
	}
}

// TestSilkToWAVSilkDecodeFailureKeepsRawErr 头部分形似 SILK 但解码失败时，
// 仍保留底层 silkcc 错误供日志排查。
func TestSilkToWAVSilkDecodeFailureKeepsRawErr(t *testing.T) {
	data := []byte("#!SILK_V3" + "\x00\x00\x00")
	_, err := silkToWAV(data)
	if err == nil {
		t.Fatal("非法 SILK 载荷应报错")
	}
	var uf *UnsupportedVoiceFormatError
	if !errors.As(err, &uf) {
		t.Fatalf("错误应为 *UnsupportedVoiceFormatError, 实际 %T: %v", err, err)
	}
	if uf.Err == nil {
		t.Error("silk 解码失败时应保留底层错误")
	}
}
