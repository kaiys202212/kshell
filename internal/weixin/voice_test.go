package weixin

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
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
		{"OPUS 原始帧", []byte("#!OPUS_RAW_V1\x02\x01\x00"), audioFormatOpusRaw},
		// 魔数不完整时不能误判为 OPUS。
		{"OPUS 魔数前缀不足", []byte("#!OPUS_RAW"), audioFormatUnknown},
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
// 必须保留「解码失败」语义，不能被误归因为「格式不受支持」（否则回执会
// 出现「格式不受支持（SILK）」的自相矛盾表述）。
func TestSilkToWAVSilkDecodeFailureKeepsRawErr(t *testing.T) {
	data := []byte("#!SILK_V3" + "\x00\x00\x00")
	_, err := silkToWAV(data)
	if err == nil {
		t.Fatal("非法 SILK 载荷应报错")
	}
	var uf *UnsupportedVoiceFormatError
	if errors.As(err, &uf) {
		t.Fatalf("SILK 解码失败不应归因为格式不受支持: %v", err)
	}
	if !strings.Contains(err.Error(), "SILK 解码") {
		t.Errorf("错误应保留 SILK 解码语义: %v", err)
	}
}

// opusSilencePacket 是 3 字节 Opus 包（TOC 0xB8 + 0xFF 0xFE），解码为 20ms
// 静音。用它合成测试容器，不依赖真实录音。
var opusSilencePacket = []byte{0xB8, 0xFF, 0xFE}

// opusRawStream 按上游容器格式拼一份 OPUS 原始帧载荷：
// "#!OPUS_RAW_V1" + 3 字节版本头 + 重复 [uint16LE 长度 + Opus 包]。
func opusRawStream(frames ...[]byte) []byte {
	out := append([]byte("#!OPUS_RAW_V1"), 0x02, 0x01, 0x00)
	for _, f := range frames {
		out = append(out, byte(len(f)), byte(len(f)>>8))
		out = append(out, f...)
	}
	return out
}

// TestParseOpusRawFrames 校验容器按 uint16LE 长度逐帧消费。
func TestParseOpusRawFrames(t *testing.T) {
	data := opusRawStream(opusSilencePacket, opusSilencePacket, opusSilencePacket)
	frames, err := parseOpusRawFrames(data)
	if err != nil {
		t.Fatalf("parseOpusRawFrames: %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("帧数 = %d, 期望 3", len(frames))
	}
	for i, f := range frames {
		if !bytes.Equal(f, opusSilencePacket) {
			t.Errorf("帧 %d = % x, 期望 % x", i, f, opusSilencePacket)
		}
	}
}

// TestParseOpusRawFramesErrors 覆盖畸形容器：必须返回明确错误且不 panic。
func TestParseOpusRawFramesErrors(t *testing.T) {
	header := append([]byte("#!OPUS_RAW_V1"), 0x02, 0x01, 0x00)
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"魔数不符", []byte("#!OPUS_RAW"), "魔数"},
		{"只有头没有帧", header, "没有 Opus 帧"},
		{"帧长度越界", append(append([]byte{}, header...), 0x03, 0x00, 0xB8, 0xFF), "越界"},
		{"长度前缀后无载荷", append(append([]byte{}, header...), 0xFF, 0xFF), "越界"},
		{"残余字节", append(opusRawStream(opusSilencePacket), 0x01), "残余"},
		{"空帧", append(append([]byte{}, header...), 0x00, 0x00), "空 Opus 帧"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseOpusRawFrames(tc.data)
			if err == nil {
				t.Fatal("畸形容器应报错")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误 %q 应包含 %q", err, tc.want)
			}
		})
	}
}

// repeatPackets 把同一 Opus 包重复 n 份，供合成多帧容器。
func repeatPackets(p []byte, n int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = p
	}
	return out
}

// TestDecodeVoiceOpusToWAV 校验 Opus 容器解码输出与 SILK 一致的 WAV 形态：
// 24kHz 单声道 16-bit PCM，每帧 20ms（480 采样）。
func TestDecodeVoiceOpusToWAV(t *testing.T) {
	const frames = 5
	stream := opusRawStream(repeatPackets(opusSilencePacket, frames)...)
	wav, err := decodeVoice(stream)
	if err != nil {
		t.Fatalf("decodeVoice: %v", err)
	}
	pcm := assertWAV(t, wav, 24000)
	if want := frames * 480 * 2; len(pcm) != want {
		t.Errorf("PCM 长度 = %d, 期望 %d", len(pcm), want)
	}
}

// TestDecodeVoiceOpusBadPacket 单帧解码失败必须保留「解码失败」语义，
// 不能误归因为「格式不受支持」。
func TestDecodeVoiceOpusBadPacket(t *testing.T) {
	stream := opusRawStream([]byte{0xFF})
	_, err := decodeVoice(stream)
	if err == nil {
		t.Fatal("非法 Opus 包应报错")
	}
	var uf *UnsupportedVoiceFormatError
	if errors.As(err, &uf) {
		t.Fatalf("Opus 解码失败不应归因为格式不受支持: %v", err)
	}
	if !strings.Contains(err.Error(), "OPUS 解码") {
		t.Errorf("错误应保留 OPUS 解码语义: %v", err)
	}
}
