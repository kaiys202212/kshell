package weixin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// decodeBody 把请求体解析为通用 map，便于断言字段。
func decodeBody(t *testing.T, r io.Reader) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(r).Decode(&m); err != nil {
		t.Fatalf("解析请求体失败: %v", err)
	}
	return m
}

func TestGetQRCode(t *testing.T) {
	var gotPath, gotBotType, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBotType = r.URL.Query().Get("bot_type")
		gotAuth = r.Header.Get("Authorization")
		io.WriteString(w, `{"qrcode_img_content":"https://login.weixin/abc","qrcode":"qr-key-1"}`)
	}))
	defer srv.Close()

	c := NewClient("tok-1", srv.URL)
	qr, err := c.GetQRCode(context.Background())
	if err != nil {
		t.Fatalf("GetQRCode: %v", err)
	}
	if gotPath != "/ilink/bot/get_bot_qrcode" {
		t.Errorf("路径 = %q, 期望 get_bot_qrcode", gotPath)
	}
	if gotBotType != "3" {
		t.Errorf("bot_type = %q, 期望 3", gotBotType)
	}
	if !strings.HasPrefix(gotAuth, "Bearer tok-1") {
		t.Errorf("Authorization = %q, 期望 Bearer tok-1", gotAuth)
	}
	if qr.ImgContent != "https://login.weixin/abc" || qr.Key != "qr-key-1" {
		t.Errorf("二维码字段解析错误: %+v", qr)
	}
}

func TestPollQRCodeStatus(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/ilink/bot/get_qrcode_status" {
			t.Errorf("路径 = %q", r.URL.Path)
		}
		if calls < 3 {
			io.WriteString(w, `{"status":1}`)
			return
		}
		io.WriteString(w, `{"bot_token":"bt-new","baseurl":"https://other.example.com"}`)
	}))
	defer srv.Close()

	c := NewClient("tok-1", srv.URL)
	for i := 0; i < 2; i++ {
		st, err := c.PollQRCodeStatus(context.Background(), "qr-key-1")
		if err != nil {
			t.Fatalf("第 %d 次轮询: %v", i+1, err)
		}
		if st.Scanned {
			t.Errorf("第 %d 次轮询不应已扫码", i+1)
		}
	}
	st, err := c.PollQRCodeStatus(context.Background(), "qr-key-1")
	if err != nil {
		t.Fatalf("第三次轮询: %v", err)
	}
	if !st.Scanned {
		t.Error("含 bot_token 应判定为已扫码")
	}
	if st.BotToken != "bt-new" || st.BaseURL != "https://other.example.com" {
		t.Errorf("扫码结果解析错误: %+v", st)
	}
}

func TestGetUpdates(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/getupdates" {
			t.Errorf("路径 = %q", r.URL.Path)
		}
		gotBody = decodeBody(t, r.Body)
		io.WriteString(w, `{
			"msgs":[
				{"message_type":1,"from_user_id":"u1","context_token":"ct1","create_time_ms":1792280448633,"item_list":[{"type":1,"text_item":{"text":"你"}}]},
				{"message_type":2,"from_user_id":"u2","context_token":"ct2","item_list":[{"type":1,"text_item":{"text":"bot 自发"}}]},
				{"message_type":1,"from_user_id":"u3","context_token":"ct3","item_list":[{"type":1,"text_item":{"text":"好"}},{"type":1,"text_item":{"text":"呀"}}]}
			],
			"get_updates_buf":"cursor-next"
		}`)
	}))
	defer srv.Close()

	c := NewClient("tok-1", srv.URL)
	msgs, cursor, err := c.GetUpdates(context.Background(), "cursor-old")
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if gotBody["get_updates_buf"] != "cursor-old" {
		t.Errorf("请求游标 = %v, 期望 cursor-old", gotBody["get_updates_buf"])
	}
	if _, ok := gotBody["base_info"]; !ok {
		t.Error("请求体缺少 base_info")
	}
	if cursor != "cursor-next" {
		t.Errorf("新游标 = %q, 期望 cursor-next", cursor)
	}
	if len(msgs) != 2 {
		t.Fatalf("消息数 = %d, 期望 2（bot 自发应被过滤）", len(msgs))
	}
	if msgs[0].FromUserID != "u1" || msgs[0].ContextToken != "ct1" || msgs[0].Text != "你" {
		t.Errorf("第一条消息解析错误: %+v", msgs[0])
	}
	// create_time_ms 供「按消息时间丢弃重启前积压」判定，必须解析。
	if msgs[0].CreateMs != 1792280448633 {
		t.Errorf("CreateMs = %d, 期望 1792280448633", msgs[0].CreateMs)
	}
	if msgs[1].FromUserID != "u3" || msgs[1].Text != "好呀" {
		t.Errorf("第二条消息文本拼接错误: %+v", msgs[1])
	}
}

func TestGetUpdatesAttachments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{
			"msgs":[
				{"message_type":1,"from_user_id":"u1","context_token":"ct1","create_time_ms":1792280448633,
				 "item_list":[
					{"type":4,"file_item":{"media":{"encrypt_query_param":"eqp-f","aes_key":"a2V5ZmlsZWhleGMyNTY="},"file_name":"需求.pdf","len":1024,"md5":"abc"}},
					{"type":2,"image_item":{"media":{"encrypt_query_param":"eqp-i","aes_key":"cmF3MTZieXRlcw=="},"len":2048}}
				 ]},
				{"message_type":1,"from_user_id":"u2","context_token":"ct2",
				 "item_list":[{"type":3,"voice_item":{"text":"语音转写内容"}}]}
			],
			"get_updates_buf":"cursor-a"
		}`)
	}))
	defer srv.Close()

	c := NewClient("tok-1", srv.URL)
	msgs, _, err := c.GetUpdates(context.Background(), "")
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("消息数 = %d, 期望 2", len(msgs))
	}
	// 文件 + 图片两条附件。
	if len(msgs[0].Attachments) != 2 {
		t.Fatalf("附件数 = %d, 期望 2: %+v", len(msgs[0].Attachments), msgs[0].Attachments)
	}
	f, im := msgs[0].Attachments[0], msgs[0].Attachments[1]
	if f.Kind != "file" || f.Name != "需求.pdf" || f.EncryptQueryParam != "eqp-f" || f.AESKey != "a2V5ZmlsZWhleGMyNTY=" || f.Size != 1024 {
		t.Errorf("文件附件解析错误: %+v", f)
	}
	if im.Kind != "image" || im.Name != "image.jpg" || im.EncryptQueryParam != "eqp-i" || im.AESKey != "cmF3MTZieXRlcw==" {
		t.Errorf("图片附件解析错误: %+v", im)
	}
	// 语音无 text 字段可用（ASR 转写由桥层取 voice_item.text），附件为空、文本为空。
	if len(msgs[1].Attachments) != 0 {
		t.Errorf("语音消息不应产生附件: %+v", msgs[1].Attachments)
	}
	if msgs[1].Text != "语音转写内容" {
		t.Errorf("语音 ASR 转写应作为文本透传: %+v", msgs[1])
	}
}

func TestGetUpdatesVoice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{
			"msgs":[
				{"message_type":1,"from_user_id":"u1","context_token":"ct1",
				 "item_list":[{"type":3,"voice_item":{"text":"","media":{"encrypt_query_param":"eqp-v","aes_key":"aGV4MzJjaGFycw=="},"len":8192}}]},
				{"message_type":1,"from_user_id":"u2","context_token":"ct2",
				 "item_list":[{"type":3,"voice_item":{"text":"服务端转写内容","media":{"encrypt_query_param":"eqp-v2","aes_key":"aGV4MzJjaGFycw=="}},"type":3}]}
			],
			"get_updates_buf":"cursor-v"
		}`)
	}))
	defer srv.Close()

	c := NewClient("tok-1", srv.URL)
	msgs, _, err := c.GetUpdates(context.Background(), "")
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("消息数 = %d, 期望 2", len(msgs))
	}
	// 无转写：Voice 携带媒体信息，Text 为空（桥层据此走模型转写）。
	if msgs[0].Text != "" {
		t.Errorf("无转写语音 Text 应为空: %q", msgs[0].Text)
	}
	if msgs[0].Voice == nil {
		t.Fatal("无转写语音应填充 Voice")
	}
	if msgs[0].Voice.Kind != "voice" || msgs[0].Voice.EncryptQueryParam != "eqp-v" ||
		msgs[0].Voice.AESKey != "aGV4MzJjaGFycw==" || msgs[0].Voice.Size != 8192 {
		t.Errorf("Voice 媒体信息解析错误: %+v", msgs[0].Voice)
	}
	if len(msgs[0].Attachments) != 0 {
		t.Errorf("语音不应进附件列表: %+v", msgs[0].Attachments)
	}
	// 有转写：Text 直通，Voice 媒体信息同样保留（桥层优先用 Text）。
	if msgs[1].Text != "服务端转写内容" {
		t.Errorf("ASR 转写应作为文本透传: %+v", msgs[1])
	}
	if msgs[1].Voice == nil || msgs[1].Voice.EncryptQueryParam != "eqp-v2" {
		t.Errorf("有转写语音的 Voice 媒体信息缺失: %+v", msgs[1].Voice)
	}
}

func TestSendMessage(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/sendmessage" {
			t.Errorf("路径 = %q", r.URL.Path)
		}
		gotBody = decodeBody(t, r.Body)
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	c := NewClient("tok-1", srv.URL)
	if err := c.SendMessage(context.Background(), "u9", "ct9", "回复内容"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	msg, ok := gotBody["msg"].(map[string]any)
	if !ok {
		t.Fatalf("请求体缺少外层 msg 包裹: %v", gotBody)
	}
	if msg["to_user_id"] != "u9" {
		t.Errorf("to_user_id = %v, 期望 u9", msg["to_user_id"])
	}
	if msg["message_type"] != float64(2) {
		t.Errorf("message_type = %v, 期望 2", msg["message_type"])
	}
	if msg["message_state"] != float64(2) {
		t.Errorf("message_state = %v, 期望 2", msg["message_state"])
	}
	if msg["context_token"] != "ct9" {
		t.Errorf("context_token = %v, 期望 ct9", msg["context_token"])
	}
	if msg["from_user_id"] != "" {
		t.Errorf("from_user_id = %v, 期望空串", msg["from_user_id"])
	}
	if cid, _ := msg["client_id"].(string); cid == "" {
		t.Error("client_id 未生成")
	}
	items, ok := msg["item_list"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("item_list 解析错误: %v", msg["item_list"])
	}
	item := items[0].(map[string]any)
	if item["type"] != float64(1) {
		t.Errorf("item type = %v, 期望 1", item["type"])
	}
	textItem := item["text_item"].(map[string]any)
	if textItem["text"] != "回复内容" {
		t.Errorf("text = %v, 期望 回复内容", textItem["text"])
	}
}

func TestSendTyping(t *testing.T) {
	var configBody, typingBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ilink/bot/getconfig":
			configBody = decodeBody(t, r.Body)
			io.WriteString(w, `{"typing_ticket":"ticket-77"}`)
		case "/ilink/bot/sendtyping":
			typingBody = decodeBody(t, r.Body)
			io.WriteString(w, `{}`)
		default:
			t.Errorf("意外路径 %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := NewClient("tok-1", srv.URL)
	if err := c.SendTyping(context.Background(), "u5", "ct5", 1); err != nil {
		t.Fatalf("SendTyping: %v", err)
	}
	if configBody["ilink_user_id"] != "u5" || configBody["context_token"] != "ct5" {
		t.Errorf("getconfig 请求体错误: %v", configBody)
	}
	if typingBody["typing_ticket"] != "ticket-77" {
		t.Errorf("typing_ticket = %v, 期望 ticket-77", typingBody["typing_ticket"])
	}
	if typingBody["status"] != float64(1) {
		t.Errorf("status = %v, 期望 1", typingBody["status"])
	}
	if typingBody["ilink_user_id"] != "u5" {
		t.Errorf("sendtyping ilink_user_id = %v, 期望 u5", typingBody["ilink_user_id"])
	}
}

func TestSendTypingNoTicket(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	c := NewClient("tok-1", srv.URL)
	if err := c.SendTyping(context.Background(), "u5", "ct5", 2); err == nil {
		t.Error("拿不到 typing_ticket 应返回 error")
	}
}

func TestSessionExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 真实服务端错误形态（2026-10-17 curl 实测）：errcode 字段，非 code。
		io.WriteString(w, `{"errcode":-14,"errmsg":"session timeout"}`)
	}))
	defer srv.Close()

	c := NewClient("tok-1", srv.URL)
	_, _, err := c.GetUpdates(context.Background(), "cur")
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("errcode=-14 应映射为 ErrSessionExpired, 实际: %v", err)
	}
	if err := c.SendMessage(context.Background(), "u", "ct", "t"); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("SendMessage 同样应映射 ErrSessionExpired, 实际: %v", err)
	}
}

func TestOtherErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"errcode":-99,"errmsg":"boom"}`)
	}))
	defer srv.Close()

	c := NewClient("tok-1", srv.URL)
	err := c.SendMessage(context.Background(), "u", "ct", "t")
	if err == nil || errors.Is(err, ErrSessionExpired) {
		t.Fatalf("非 -14 错误码不应映射为 ErrSessionExpired: %v", err)
	}
	if !strings.Contains(err.Error(), "-99") {
		t.Errorf("错误信息应包含 errcode: %v", err)
	}
}

func TestUINHeader(t *testing.T) {
	var uin, authType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uin = r.Header.Get("X-WECHAT-UIN")
		authType = r.Header.Get("AuthorizationType")
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	c := NewClient("tok-1", srv.URL)
	if err := c.SendMessage(context.Background(), "u", "ct", "t"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if uin == "" {
		t.Fatal("缺少 X-WECHAT-UIN 头")
	}
	// 参考实现（openclaw-weixin 1.0.2 api.ts）：随机 uint32 的十进制字符串再 base64，
	// 不是 4 字节二进制直接 base64。解码后应是全数字的 ASCII。
	raw, err := base64.StdEncoding.DecodeString(uin)
	if err != nil {
		t.Fatalf("X-WECHAT-UIN 不是合法 base64: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("X-WECHAT-UIN 解码后为空")
	}
	for _, b := range raw {
		if b < '0' || b > '9' {
			t.Fatalf("X-WECHAT-UIN 解码后 %q 非十进制数字串", raw)
		}
	}
	// 2026-10-17 真机联调证实：不带该头一律 errcode=-14 session timeout。
	if authType != "ilink_bot_token" {
		t.Fatalf("AuthorizationType = %q, 期望 ilink_bot_token", authType)
	}
}

func TestServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient("tok-1", srv.URL)
	if err := c.SendMessage(context.Background(), "u", "ct", "t"); err == nil {
		t.Error("服务端 500 应返回 error")
	}
}
