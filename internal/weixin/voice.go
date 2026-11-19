package weixin

import (
	"bytes"
	"encoding/binary"
	"fmt"

	silk "github.com/yoshino-s/silk-go"
)

// voiceSampleRate 是微信语音的目标采样率（SILK 解码输出，24kHz 单声道）。
const voiceSampleRate = 24000

// 语音载荷格式标识：silkcc 只解码 SILK v3，其余格式在进入解码器前识别，
// 让失败回执与日志指向真实原因，而不是笼统的 silkcc -1000。
const (
	audioFormatSilk    = "SILK"
	audioFormatAMR     = "AMR"
	audioFormatMP3     = "MP3"
	audioFormatM4A     = "M4A"
	audioFormatOGG     = "OGG"
	audioFormatWAV     = "WAV"
	audioFormatUnknown = "unknown"
)

// UnsupportedVoiceFormatError 表示解密后的语音载荷不是可解码的 SILK v3。
// Format 是嗅探到的实际格式，Head 是前 16 字节，DumpPath 是失败样本落盘
// 路径（由桥层填充），Err 仅在“头部分形似 SILK 但解码失败”时保留底层错误。
type UnsupportedVoiceFormatError struct {
	Format   string
	Head     []byte
	DumpPath string
	Err      error
}

func (e *UnsupportedVoiceFormatError) Error() string {
	msg := fmt.Sprintf("语音格式不受支持（%s，首部 %X）", e.Format, e.Head)
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	if e.DumpPath != "" {
		msg += "，原始载荷已保存: " + e.DumpPath
	}
	return msg
}

// Unwrap 让调用方可继续用 errors.Is/As 追溯底层解码错误。
func (e *UnsupportedVoiceFormatError) Unwrap() error { return e.Err }

// voiceHeadBytes 取前 16 字节用于日志与回执定位；不足时原样返回。
func voiceHeadBytes(data []byte) []byte {
	const n = 16
	if len(data) > n {
		return data[:n]
	}
	return data
}

// detectAudioFormat 按魔数识别解密后的语音载荷。微信语音常见 SILK v3；
// 其它格式（AMR/MP3/M4A/OGG/WAV）当前不直接解码，但识别出来即可给出
// 可定位的回执，避免把“格式不支持”误报成“转写服务失败”。
func detectAudioFormat(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0x02}) && bytes.HasPrefix(data[1:], []byte("#!SILK_V3")):
		return audioFormatSilk
	case bytes.HasPrefix(data, []byte("#!SILK_V3")):
		return audioFormatSilk
	case bytes.HasPrefix(data, []byte("#!AMR")):
		return audioFormatAMR
	case bytes.HasPrefix(data, []byte("ID3")):
		return audioFormatMP3
	// MP3 帧同步头：11 位全 1。
	case len(data) >= 2 && data[0] == 0xFF && data[1]&0xE0 == 0xE0:
		return audioFormatMP3
	// ISO BMFF（m4a/aac）：offset 4 起是 ftyp box。
	case len(data) >= 12 && bytes.Equal(data[4:8], []byte("ftyp")):
		return audioFormatM4A
	case bytes.HasPrefix(data, []byte("OggS")):
		return audioFormatOGG
	case len(data) >= 12 && bytes.HasPrefix(data, []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WAVE")):
		return audioFormatWAV
	}
	return audioFormatUnknown
}

// silkToWAV 把微信语音的 SILK 流解码为 WAV（16-bit LE 单声道 PCM + 44 字节头），
// 供 ASR 端点消费。微信流常带 STX(0x02) 前缀 + "#!SILK_V3" 头（silk-go 的
// TencentCompat 编码即此形态），归一化剥前缀后交给解码器；格式与真机不符时
// 只需改这里。yoshino-s/silk-go 为 ccgo 转译的纯 Go 实现，无 cgo 依赖。
// 注意：silk-go 的 ccgo 代码在 `go test -race` 下会触发 checkptr fatal（非本包
// 竞态）；本包排查竞态时跳过语音相关用例或改用无 -race 的对照基线。
func silkToWAV(data []byte) ([]byte, error) {
	if bytes.HasPrefix(data, []byte{0x02}) && bytes.HasPrefix(data[1:], []byte("#!SILK_V3")) {
		data = data[1:]
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("空 SILK 流")
	}
	if format := detectAudioFormat(data); format != audioFormatSilk {
		return nil, &UnsupportedVoiceFormatError{Format: format, Head: voiceHeadBytes(data)}
	}
	pcm, err := silk.DecodePCM(data, silk.DecodeOptions{SampleRateHz: voiceSampleRate})
	if err != nil {
		return nil, &UnsupportedVoiceFormatError{
			Format: audioFormatSilk,
			Head:   voiceHeadBytes(data),
			Err:    fmt.Errorf("SILK 解码: %w", err),
		}
	}
	if len(pcm) == 0 {
		return nil, fmt.Errorf("SILK 解码结果为空")
	}
	return wavFromPCM(pcm, voiceSampleRate), nil
}

// wavFromPCM 给 16-bit LE 单声道 PCM 拼 44 字节 RIFF/WAV 头。
func wavFromPCM(pcm []byte, sampleRate int) []byte {
	buf := make([]byte, 44+len(pcm))
	copy(buf[0:], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:], uint32(36+len(pcm)))
	copy(buf[8:], "WAVE")
	copy(buf[12:], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:], 16)                   // fmt 块长
	binary.LittleEndian.PutUint16(buf[20:], 1)                    // PCM
	binary.LittleEndian.PutUint16(buf[22:], 1)                    // 单声道
	binary.LittleEndian.PutUint32(buf[24:], uint32(sampleRate))   // 采样率
	binary.LittleEndian.PutUint32(buf[28:], uint32(sampleRate*2)) // 字节率 = rate*2
	binary.LittleEndian.PutUint16(buf[32:], 2)                    // 块对齐
	binary.LittleEndian.PutUint16(buf[34:], 16)                   // 位深
	copy(buf[36:], "data")
	binary.LittleEndian.PutUint32(buf[40:], uint32(len(pcm)))
	copy(buf[44:], pcm)
	return buf
}
