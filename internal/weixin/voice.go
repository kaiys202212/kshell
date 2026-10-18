package weixin

import (
	"bytes"
	"encoding/binary"
	"fmt"

	silk "github.com/yoshino-s/silk-go"
)

// voiceSampleRate 是微信语音的目标采样率（SILK 解码输出，24kHz 单声道）。
const voiceSampleRate = 24000

// silkToWAV 把微信语音的 SILK 流解码为 WAV（16-bit LE 单声道 PCM + 44 字节头），
// 供 ASR 端点消费。微信流常带 STX(0x02) 前缀 + "#!SILK_V3" 头（silk-go 的
// TencentCompat 编码即此形态），归一化剥前缀后交给解码器；格式与真机不符时
// 只需改这里。yoshino-s/silk-go 为 ccgo 转译的纯 Go 实现，无 cgo 依赖。
func silkToWAV(data []byte) ([]byte, error) {
	if bytes.HasPrefix(data, []byte{0x02}) && bytes.HasPrefix(data[1:], []byte("#!SILK_V3")) {
		data = data[1:]
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("空 SILK 流")
	}
	pcm, err := silk.DecodePCM(data, silk.DecodeOptions{SampleRateHz: voiceSampleRate})
	if err != nil {
		return nil, fmt.Errorf("SILK 解码: %w", err)
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
	binary.LittleEndian.PutUint32(buf[16:], 16)                      // fmt 块长
	binary.LittleEndian.PutUint16(buf[20:], 1)                       // PCM
	binary.LittleEndian.PutUint16(buf[22:], 1)                       // 单声道
	binary.LittleEndian.PutUint32(buf[24:], uint32(sampleRate))      // 采样率
	binary.LittleEndian.PutUint32(buf[28:], uint32(sampleRate*2))    // 字节率 = rate*2
	binary.LittleEndian.PutUint16(buf[32:], 2)                       // 块对齐
	binary.LittleEndian.PutUint16(buf[34:], 16)                      // 位深
	copy(buf[36:], "data")
	binary.LittleEndian.PutUint32(buf[40:], uint32(len(pcm)))
	copy(buf[44:], pcm)
	return buf
}
