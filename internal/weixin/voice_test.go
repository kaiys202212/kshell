package weixin

import (
	"bytes"
	"encoding/binary"
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
