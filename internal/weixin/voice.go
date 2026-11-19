package weixin

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/pion/opus"
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
	audioFormatOpusRaw = "OPUS"
	audioFormatUnknown = "unknown"
)

// opusRawMagic 是上游媒体 CDN 对语音下发的「Opus 原始帧」容器魔数
// （2026-11-19 真机样本确认）：魔数后跟 3 字节版本头（当前为 02 01 00，
// 语义未公开，按固定长度跳过），再是重复的 [uint16LE 长度 + Opus 包]。
const (
	opusRawMagic     = "#!OPUS_RAW_V1"
	opusRawHeaderLen = 3
)

// UnsupportedVoiceFormatError 表示解密后的语音载荷不是可解码的格式
//（当前支持 SILK v3 与 OPUS_RAW_V1）。
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
	case bytes.HasPrefix(data, []byte(opusRawMagic)):
		return audioFormatOpusRaw
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
		// 头部分形似 SILK 但解码失败：保留「解码失败」语义（区别于格式
		// 不受支持），首部字节供日志定位损坏/截断的实际位置。
		return nil, fmt.Errorf("SILK 解码失败（首部 %X）: %w", voiceHeadBytes(data), err)
	}
	if len(pcm) == 0 {
		return nil, fmt.Errorf("SILK 解码结果为空")
	}
	return wavFromPCM(pcm, voiceSampleRate), nil
}

// decodeVoice 按嗅探到的格式分派解码：SILK 走既有 silkToWAV（行为不变），
// OPUS_RAW_V1 走 decodeOpusRaw，其余格式返回可归因的
// UnsupportedVoiceFormatError（供回执与失败样本落盘）。
func decodeVoice(data []byte) ([]byte, error) {
	switch format := detectAudioFormat(data); format {
	case audioFormatSilk:
		return silkToWAV(data)
	case audioFormatOpusRaw:
		pcm, err := decodeOpusRaw(data)
		if err != nil {
			return nil, err
		}
		return wavFromPCM(pcm, voiceSampleRate), nil
	default:
		return nil, &UnsupportedVoiceFormatError{Format: format, Head: voiceHeadBytes(data)}
	}
}

// parseOpusRawFrames 把 OPUS_RAW_V1 容器拆成 Opus 包切片：校验魔数与版本头，
// 其后按 uint16LE 长度逐帧消费。长度越界、残余字节、空帧都返回明确错误，
// 不做任何越界读取（异常载荷不能 panic）。
func parseOpusRawFrames(data []byte) ([][]byte, error) {
	magic := []byte(opusRawMagic)
	if !bytes.HasPrefix(data, magic) {
		return nil, fmt.Errorf("OPUS 原始帧魔数不符（首部 %X）", voiceHeadBytes(data))
	}
	off := len(magic)
	if len(data) < off+opusRawHeaderLen {
		return nil, fmt.Errorf("OPUS 原始帧头不完整（共 %d 字节）", len(data))
	}
	off += opusRawHeaderLen
	var frames [][]byte
	for off < len(data) {
		if len(data)-off < 2 {
			return nil, fmt.Errorf("OPUS 原始帧残余 %d 字节长度前缀", len(data)-off)
		}
		n := int(binary.LittleEndian.Uint16(data[off:]))
		off += 2
		if n == 0 {
			return nil, fmt.Errorf("空 Opus 帧（第 %d 帧）", len(frames)+1)
		}
		if n > len(data)-off {
			return nil, fmt.Errorf("OPUS 原始帧第 %d 帧长度 %d 越界（剩余 %d 字节）",
				len(frames)+1, n, len(data)-off)
		}
		frames = append(frames, data[off:off+n])
		off += n
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("OPUS 原始帧没有 Opus 帧")
	}
	return frames, nil
}

// decodeOpusRaw 把 OPUS_RAW_V1 容器解码为 16-bit LE 单声道 PCM（24kHz，
// 与 SILK 输出一致）。pion/opus 是纯 Go 实现，无 cgo 依赖。单帧失败保留
//「解码失败」语义并带帧号与首部字节，区别于「格式不受支持」。
func decodeOpusRaw(data []byte) ([]byte, error) {
	frames, err := parseOpusRawFrames(data)
	if err != nil {
		return nil, err
	}
	dec, err := opus.NewDecoderWithOutput(voiceSampleRate, 1)
	if err != nil {
		return nil, fmt.Errorf("初始化 OPUS 解码器: %w", err)
	}
	// 单包最长 120ms；24kHz 下 2880 采样足够容纳任意合法帧。
	buf := make([]int16, voiceSampleRate*120/1000)
	pcm := make([]int16, 0, len(frames)*voiceSampleRate*20/1000)
	for i, f := range frames {
		n, err := dec.DecodeToInt16(f, buf)
		if err != nil {
			return nil, fmt.Errorf("OPUS 解码失败（第 %d 帧，首部 %X）: %w", i+1, voiceHeadBytes(f), err)
		}
		pcm = append(pcm, buf[:n]...)
	}
	if len(pcm) == 0 {
		return nil, fmt.Errorf("OPUS 解码结果为空")
	}
	out := make([]byte, len(pcm)*2)
	for i, s := range pcm {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(s))
	}
	return out, nil
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
