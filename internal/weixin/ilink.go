package weixin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ErrSessionExpired 表示服务端返回 code=-14（iLink 会话过期），需重新扫码登录。
var ErrSessionExpired = errors.New("iLink 会话过期")

// channelVersion 是协议要求的 base_info 渠道版本号（2026-03 抓包值）。
const channelVersion = "1.0.2"

// Client 是 iLink 协议客户端。零值不可用，须经 NewClient 创建。
type Client struct {
	HTTP    *http.Client
	Token   string
	BaseURL string
	// MediaBase 是媒体 CDN 基础地址（下载附件用）；空串时用默认值。测试注入点。
	MediaBase string
}

// NewClient 创建 iLink 客户端；baseURL 为空时用官方默认域名。
func NewClient(token, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://ilinkai.weixin.qq.com"
	}
	return &Client{
		HTTP:      &http.Client{Timeout: 40 * time.Second},
		Token:     token,
		BaseURL:   baseURL,
		MediaBase: mediaBaseDefault,
	}
}

// baseInfo 是所有请求体都带的渠道信息。
type baseInfo struct {
	ChannelVersion string `json:"channel_version"`
}

// CDNMedia 是所有媒体 item 共用的 CDN 引用：下载参数与 base64 AES 密钥。
type CDNMedia struct {
	EncryptQueryParam string `json:"encrypt_query_param"`
	AESKey            string `json:"aes_key"`
}

// apiResp 是服务端响应的通用外壳，errcode 非 0 表示错误。
// 2026-10-17 真机联调证实字段是 errcode（非早期拆解记录的 code）。
type apiResp struct {
	Errcode int    `json:"errcode"`
	Errmsg  string `json:"errmsg"`
}

// post 发送 JSON POST 请求并解析响应到 out（可为 nil）。
func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

// get 发送 GET 请求并解析响应到 out（可为 nil）。
func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("序列化请求体: %w", err)
		}
		rd = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	// AuthorizationType 告知服务端按 bot_token 通道鉴权：真机联调证实不带该头
	// 一律返回 errcode=-14 session timeout（登录两接口不校验，故绑定能成功）。
	req.Header.Set("AuthorizationType", "ilink_bot_token")
	// X-WECHAT-UIN：随机 uint32 的十进制字符串再 base64（参照 openclaw-weixin
	// 1.0.2 api.ts buildHeaders；不是 4 字节二进制直接 base64）。
	var uinRaw [4]byte
	_, _ = rand.Read(uinRaw[:])
	uin := binary.BigEndian.Uint32(uinRaw[:])
	req.Header.Set("X-WECHAT-UIN", base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(uin), 10))))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("iLink %s: HTTP %d", path, resp.StatusCode)
	}
	var api apiResp
	// 先整体解码到 api，再把剩余字段解到 out；简单起见解码两次代价可接受。
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &api); err != nil {
		return fmt.Errorf("解析 iLink 响应: %w", err)
	}
	if api.Errcode == -14 {
		return ErrSessionExpired
	}
	if api.Errcode != 0 {
		return fmt.Errorf("iLink 错误 errcode=%d: %s", api.Errcode, api.Errmsg)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("解析 iLink 响应: %w", err)
		}
	}
	return nil
}

// 各接口的请求超时（长轮询按协议允许的挂起时长设置）。
const (
	timeoutGetQRCode     = 40 * time.Second
	timeoutPollQRStatus  = 35 * time.Second
	timeoutGetUpdates    = 40 * time.Second
	timeoutSendMessage   = 15 * time.Second
	timeoutTypingRelated = 10 * time.Second
)

// GetQRCode 获取扫码登录二维码。
func (c *Client) GetQRCode(ctx context.Context) (QRCode, error) {
	ctx, cancel := context.WithTimeout(ctx, timeoutGetQRCode)
	defer cancel()
	var out struct {
		ImgContent string `json:"qrcode_img_content"`
		Key        string `json:"qrcode"`
	}
	if err := c.get(ctx, "/ilink/bot/get_bot_qrcode?bot_type=3", &out); err != nil {
		return QRCode{}, err
	}
	return QRCode{ImgContent: out.ImgContent, Key: out.Key}, nil
}

// PollQRCodeStatus 轮询扫码状态；响应含 bot_token 即视为扫码成功。
func (c *Client) PollQRCodeStatus(ctx context.Context, qrcodeKey string) (QRStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, timeoutPollQRStatus)
	defer cancel()
	var out struct {
		BotToken string `json:"bot_token"`
		BaseURL  string `json:"baseurl"`
	}
	if err := c.get(ctx, "/ilink/bot/get_qrcode_status?qrcode="+urlQueryEscape(qrcodeKey), &out); err != nil {
		return QRStatus{}, err
	}
	return QRStatus{BotToken: out.BotToken, BaseURL: out.BaseURL, Scanned: out.BotToken != ""}, nil
}

// GetUpdates 长轮询收消息，返回入站消息与新游标。bot 自发消息（message_type==2）已过滤。
// 附件（图片 type 2 / 文件 type 4）解析进 Attachments；语音的 ASR 转写
//（voice_item.text）非空时作为文本透传。
func (c *Client) GetUpdates(ctx context.Context, cursor string) ([]InboundMsg, string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeoutGetUpdates)
	defer cancel()
	body := map[string]any{
		"get_updates_buf": cursor,
		"base_info":       baseInfo{ChannelVersion: channelVersion},
	}
	var out struct {
		Msgs []struct {
			MessageType  int    `json:"message_type"`
			FromUserID   string `json:"from_user_id"`
			ContextToken string `json:"context_token"`
			CreateMs     int64  `json:"create_time_ms"`
			ItemList     []struct {
				Type     int `json:"type"`
				TextItem struct {
					Text string `json:"text"`
				} `json:"text_item"`
				VoiceItem struct {
					Text string `json:"text"`
				} `json:"voice_item"`
				ImageItem struct {
					Media    CDNMedia `json:"media"`
					FileSize int64    `json:"len"`
				} `json:"image_item"`
				FileItem struct {
					Media    CDNMedia `json:"media"`
					FileName string   `json:"file_name"`
					FileSize int64    `json:"len"`
					MD5      string   `json:"md5"`
				} `json:"file_item"`
			} `json:"item_list"`
		} `json:"msgs"`
		NewCursor string `json:"get_updates_buf"`
	}
	if err := c.post(ctx, "/ilink/bot/getupdates", body, &out); err != nil {
		return nil, "", err
	}
	var msgs []InboundMsg
	for _, m := range out.Msgs {
		if m.MessageType == 2 {
			continue
		}
		im := InboundMsg{FromUserID: m.FromUserID, ContextToken: m.ContextToken, CreateMs: m.CreateMs}
		for _, item := range m.ItemList {
			switch item.Type {
			case 1:
				im.Text += item.TextItem.Text
			case 3:
				// 语音：ASR 转写直接当文本（服务端已转好），其余字段（SILK 音频）不做。
				im.Text += item.VoiceItem.Text
			case 2:
				im.Attachments = append(im.Attachments, InboundAttachment{
					Kind:              "image",
					Name:              "image.jpg",
					EncryptQueryParam: item.ImageItem.Media.EncryptQueryParam,
					AESKey:            item.ImageItem.Media.AESKey,
					Size:              item.ImageItem.FileSize,
				})
			case 4:
				im.Attachments = append(im.Attachments, InboundAttachment{
					Kind:              "file",
					Name:              item.FileItem.FileName,
					EncryptQueryParam: item.FileItem.Media.EncryptQueryParam,
					AESKey:            item.FileItem.Media.AESKey,
					Size:              item.FileItem.FileSize,
					MD5:               item.FileItem.MD5,
				})
			}
		}
		msgs = append(msgs, im)
	}
	return msgs, out.NewCursor, nil
}

// SendMessage 回复文本消息；client_id 内部生成，from_user_id 恒为空。
func (c *Client) SendMessage(ctx context.Context, toUserID, contextToken, text string) error {
	ctx, cancel := context.WithTimeout(ctx, timeoutSendMessage)
	defer cancel()
	body := map[string]any{
		"msg": map[string]any{
			"to_user_id":    toUserID,
			"from_user_id":  "",
			"client_id":     newClientID(),
			"message_type":  2,
			"message_state": 2,
			"context_token": contextToken,
			"item_list": []map[string]any{
				{"type": 1, "text_item": map[string]any{"text": text}},
			},
		},
		"base_info": baseInfo{ChannelVersion: channelVersion},
	}
	return c.post(ctx, "/ilink/bot/sendmessage", body, nil)
}

// SendTyping 发送/取消输入状态（1=输入中 2=取消）。内部先 getconfig 换取 typing_ticket。
func (c *Client) SendTyping(ctx context.Context, userID, contextToken string, status int) error {
	ctx, cancel := context.WithTimeout(ctx, timeoutTypingRelated)
	defer cancel()
	cfgBody := map[string]any{
		"ilink_user_id": userID,
		"context_token": contextToken,
		"base_info":     baseInfo{ChannelVersion: channelVersion},
	}
	var cfg struct {
		TypingTicket string `json:"typing_ticket"`
	}
	if err := c.post(ctx, "/ilink/bot/getconfig", cfgBody, &cfg); err != nil {
		return err
	}
	if cfg.TypingTicket == "" {
		return fmt.Errorf("getconfig 未返回 typing_ticket，无法发送 typing 状态")
	}
	typingBody := map[string]any{
		"ilink_user_id": userID,
		"typing_ticket": cfg.TypingTicket,
		"status":        status,
		"base_info":     baseInfo{ChannelVersion: channelVersion},
	}
	return c.post(ctx, "/ilink/bot/sendtyping", typingBody, nil)
}

// newClientID 生成随机 UUID v4 风格的客户端消息 ID。
func newClientID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 随机源不可用时退化为时间戳，保证 client_id 非空。
		binary.BigEndian.PutUint64(b[:8], uint64(time.Now().UnixNano()))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// urlQueryEscape 对查询参数做 URL 转义。
func urlQueryEscape(s string) string {
	return url.QueryEscape(s)
}
