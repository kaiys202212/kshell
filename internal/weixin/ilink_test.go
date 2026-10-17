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
				{"message_type":1,"from_user_id":"u1","context_token":"ct1","item_list":[{"type":1,"text_item":{"text":"你"}}]},
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
	if msgs[1].FromUserID != "u3" || msgs[1].Text != "好呀" {
		t.Errorf("第二条消息文本拼接错误: %+v", msgs[1])
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
		io.WriteString(w, `{"code":-14,"message":"session expired"}`)
	}))
	defer srv.Close()

	c := NewClient("tok-1", srv.URL)
	_, _, err := c.GetUpdates(context.Background(), "cur")
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("code=-14 应映射为 ErrSessionExpired, 实际: %v", err)
	}
	if err := c.SendMessage(context.Background(), "u", "ct", "t"); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("SendMessage 同样应映射 ErrSessionExpired, 实际: %v", err)
	}
}

func TestOtherErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"code":-99,"message":"boom"}`)
	}))
	defer srv.Close()

	c := NewClient("tok-1", srv.URL)
	err := c.SendMessage(context.Background(), "u", "ct", "t")
	if err == nil || errors.Is(err, ErrSessionExpired) {
		t.Fatalf("非 -14 错误码不应映射为 ErrSessionExpired: %v", err)
	}
	if !strings.Contains(err.Error(), "-99") {
		t.Errorf("错误信息应包含 code: %v", err)
	}
}

func TestUINHeader(t *testing.T) {
	var uin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uin = r.Header.Get("X-WECHAT-UIN")
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
	raw, err := base64.StdEncoding.DecodeString(uin)
	if err != nil {
		t.Fatalf("X-WECHAT-UIN 不是合法 base64: %v", err)
	}
	if len(raw) != 4 {
		t.Errorf("X-WECHAT-UIN 解码后 %d 字节, 期望 4", len(raw))
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
