// Package weixin 实现微信 iLink 个人号 Bot 协议（ilinkai.weixin.qq.com，纯 HTTP/JSON）的客户端。
package weixin

// QRCode 是扫码登录的二维码数据。
type QRCode struct {
	// ImgContent 是可扫的二维码内容（URL）。
	ImgContent string
	// Key 是后续轮询扫码状态的 key。
	Key string
}

// QRStatus 是扫码轮询结果。
type QRStatus struct {
	// BotToken 是扫码成功后下发的 bot 凭证。
	BotToken string
	// BaseURL 是服务端可选下发的后续请求基础地址。
	BaseURL string
	// Scanned 表示是否已扫码成功（响应含 bot_token）。
	Scanned bool
}

// InboundAttachment 是入站消息携带的一个附件（图片/文件）。媒体内容经 CDN
// 加密存储，下载后须按 Kind 对应的编码解码 AESKey 再做 AES-128-ECB 解密。
type InboundAttachment struct {
	// Kind 取值 "file"（item type 4）或 "image"（item type 2）。
	// 决定 AESKey 的编码方式与落盘兜底名，见 ilink.go。
	Kind string
	// Name 是文件名；图片消息服务端不带名字，为空时由解析层兜底 image.jpg。
	Name string
	// EncryptQueryParam 是 CDN 下载参数（c2c/download 的查询串）。
	EncryptQueryParam string
	// AESKey 是服务端下发的 base64 AES-128 密钥（编码方式随 Kind 不同）。
	AESKey string
	// Size 是明文字节数（服务端 len 字段，可能缺失）。
	Size int64
	// MD5 是服务端给的明文 MD5（file_item.md5，可能缺失）；下载后对账用。
	MD5 string
}

// RefMsg 是引用回复携带的被引用消息信息。用户在微信里对 bot 出站消息
// 「引用回复」时，iLink 在 getupdates 载荷附上 ref_msg。
type RefMsg struct {
	// SvrID 是被引用消息的服务端 ID（sendmessage 响应回执的 svr_id），
	// 桥层据此精确归因到出站时的会话。
	SvrID string
	// Text 是被引用消息的文本（引用纯媒体消息时可能为空或仅有
	// partial_text 摘要；归因据此做【会话名】前缀匹配兜底）。
	Text string
}

// InboundMsg 是一条入站消息（bot 自发的已跳过）。
type InboundMsg struct {
	FromUserID   string
	ContextToken string
	Text         string
	// Attachments 是消息携带的附件（图片/文件）；纯文本消息为空。
	// 微信客户端把「文字+附件」拆成多条消息发送，同条消息内两者并存也合法。
	Attachments []InboundAttachment
	// Voice 是语音媒体信息（item type 3）：无论服务端是否给出 ASR 转写都填充，
	// 供桥层在 Text 为空时走模型转写；非语音消息为 nil。语音不进 Attachments
	//（它不是待转交附件，是文本的替代来源）。
	Voice *InboundAttachment
	// CreateMs 是消息创建时刻的 Unix 毫秒（服务端 create_time_ms），
	// 供「重启/重绑前的过期积压丢弃」判定；服务端缺失时为 0。
	CreateMs int64
	// Ref 是引用回复的被引用消息信息；非引用回复消息为 nil。
	// 纯媒体消息被引用时部分客户端不带 ref_msg，此时也为 nil。
	Ref *RefMsg
}
