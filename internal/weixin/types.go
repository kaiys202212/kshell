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

// InboundMsg 是一条入站消息（bot 自发的已跳过）。
type InboundMsg struct {
	FromUserID   string
	ContextToken string
	Text         string
	// CreateMs 是消息创建时刻的 Unix 毫秒（服务端 create_time_ms），
	// 供「重启/重绑前的过期积压丢弃」判定；服务端缺失时为 0。
	CreateMs int64
}
