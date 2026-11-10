package workbuddy

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// sealField 用与桌面端相同的帧结构加密明文，构造测试 fixture。
func sealField(t *testing.T, key []byte, suite int, plaintext string) encryptedField {
	t.Helper()
	sum := sha256.Sum256(key)
	keyID := hex.EncodeToString(sum[:])[:16]
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, 12)
	if err != nil {
		t.Fatal(err)
	}
	nonce := bytes.Repeat([]byte{0xA5}, 12)
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), fieldAad(keyID, suite))
	env := struct {
		Suite      int    `json:"suite"`
		KeyID      string `json:"keyId"`
		Nonce      string `json:"nonce"`
		AuthTag    string `json:"authTag"`
		Ciphertext string `json:"ciphertext"`
	}{
		Suite:      suite,
		KeyID:      keyID,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		AuthTag:    base64.StdEncoding.EncodeToString(ciphertext[len(ciphertext)-16:]),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext[:len(ciphertext)-16]),
	}
	raw, _ := json.Marshal(env)
	return encryptedField{Encrypted: 1, Envelope: base64.StdEncoding.EncodeToString(raw)}
}

func TestOpenEncryptedFieldRoundTrip(t *testing.T) {
	secret := []byte("unit-test-at-rest-secret")
	key := sha256.Sum256(secret)
	f := sealField(t, key[:], 1, "token-abc123")
	got, err := openEncryptedField(f, key[:])
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if got != "token-abc123" {
		t.Fatalf("明文不符: %q", got)
	}
}

func TestOpenEncryptedFieldWrongKey(t *testing.T) {
	f := sealField(t, bytes.Repeat([]byte{1}, 32), 1, "token")
	other := bytes.Repeat([]byte{2}, 32)
	if _, err := openEncryptedField(f, other); err == nil {
		t.Fatal("错误密钥应当解密失败")
	}
}

func TestParseAuthDocumentNested(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	doc := map[string]any{
		"account": map[string]any{"uid": "uid-1", "uin": "123"},
		"auth": map[string]any{
			"accessToken":     sealField(t, key, 1, "at-xxx"),
			"refreshToken":    sealField(t, key, 1, "rt-yyy"),
			"domain":          "www.codebuddy.cn",
			"expiresAt":       int64(1796831831969),
			"lastRefreshTime": int64(1794239832716),
		},
	}
	raw, _ := json.Marshal(doc)
	cred, err := parseAuthDocument(raw, key)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if cred.AccessToken != "at-xxx" || cred.RefreshToken != "rt-yyy" {
		t.Fatalf("token 不符: %+v", cred)
	}
	if cred.UID != "uid-1" || cred.Domain != "www.codebuddy.cn" {
		t.Fatalf("身份字段不符: %+v", cred)
	}
	if cred.ExpiresAtMs != 1796831831969 {
		t.Fatalf("过期时间不符: %d", cred.ExpiresAtMs)
	}
}

func TestParseAuthDocumentPlainFields(t *testing.T) {
	doc := map[string]any{
		"accessToken": "plain-token",
		"uid":         "uid-2",
	}
	raw, _ := json.Marshal(doc)
	cred, err := parseAuthDocument(raw, nil)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if cred.AccessToken != "plain-token" || cred.UID != "uid-2" {
		t.Fatalf("凭据不符: %+v", cred)
	}
}

func TestParseAuthDocumentNoToken(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"account": map[string]any{"uid": "u"}})
	if _, err := parseAuthDocument(raw, nil); err == nil {
		t.Fatal("无 accessToken 应当报错")
	}
}

func TestAuthFilePathsOrder(t *testing.T) {
	got := authFilePaths(`C:\L`, `C:\R`)
	want := []string{
		`C:\L\CodeBuddyExtension\Data\Public\auth\workbuddy-desktop.info`,
		`C:\L\CodeBuddyExtension\Data\Public\auth\workbuddy-desktop-ai.info`,
		`C:\R\CodeBuddyExtension\Data\Public\auth\workbuddy-desktop.info`,
		`C:\R\CodeBuddyExtension\Data\Public\auth\workbuddy-desktop-ai.info`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("路径列表不符:\n got %v\nwant %v", got, want)
	}
}

func TestExtractAtRestKey(t *testing.T) {
	origRunner, origCache := atRestKeyRunner, cachedAtRestKey
	defer func() { atRestKeyRunner, cachedAtRestKey = origRunner, origCache }()
	cachedAtRestKey = nil
	calls := 0
	atRestKeyRunner = func(exe, script string) ([]byte, error) {
		calls++
		if script == "" || exe != "fake.exe" {
			t.Fatalf("runner 参数不符: exe=%q", exe)
		}
		return []byte(`{"atRestSecretKey":"s3cret-string"}`), nil
	}
	key, err := extractAtRestKey("fake.exe")
	if err != nil {
		t.Fatalf("提取失败: %v", err)
	}
	want := sha256.Sum256([]byte("s3cret-string"))
	if !bytes.Equal(key, want[:]) {
		t.Fatal("key 应为 atRestSecretKey 字符串的 sha256")
	}
	// 第二次调用命中进程内缓存，不再 spawn exe。
	if _, err := extractAtRestKey("fake.exe"); err != nil {
		t.Fatalf("缓存读取失败: %v", err)
	}
	if calls != 1 {
		t.Fatalf("runner 应只被调用 1 次，实际 %d", calls)
	}
}

func TestLoadCredentialFromFiles(t *testing.T) {
	// fixture 用「payload 派生 key」加密：runner 返回的 atRestSecretKey
	// 经 sha256 后应正好解开文件里的 token，端到端串起 runner→派生→解密。
	secret := "k9"
	key := sha256.Sum256([]byte(secret))
	doc := map[string]any{
		"account": map[string]any{"uid": "uid-9"},
		"auth": map[string]any{
			"accessToken": sealField(t, key[:], 1, "at-999"),
			"expiresAt":   int64(1796831831969),
		},
	}
	raw, _ := json.Marshal(doc)
	origRunner, origCache := atRestKeyRunner, cachedAtRestKey
	defer func() { atRestKeyRunner, cachedAtRestKey = origRunner, origCache }()
	cachedAtRestKey = nil
	atRestKeyRunner = func(exe, script string) ([]byte, error) {
		return []byte(`{"atRestSecretKey":"` + secret + `"}`), nil
	}

	cred, err := loadCredentialFrom([]string{writeTemp(t, raw)}, "fake.exe")
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if cred.AccessToken != "at-999" || cred.UID != "uid-9" {
		t.Fatalf("凭据不符: %+v", cred)
	}
}

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	p := t.TempDir() + "\\auth.info"
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExpiryToMs(t *testing.T) {
	cases := []struct {
		in   int64
		want int64
	}{
		{0, 0},
		{-5, 0},
		{1796831831969, 1796831831969},      // 已是毫秒
		{1796831831, 1796831831000},         // 秒 → 毫秒
		{int64(0xe8d4a51000), int64(0xe8d4a51000) * 1000}, // 阈值内按秒乘
	}
	for _, c := range cases {
		if got := expiryToMs(c.in); got != c.want {
			t.Fatalf("expiryToMs(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestFieldAadShape(t *testing.T) {
	// 手工按帧规则展开，锁定字节布局，防止实现漂移。
	got := fieldAad("0123456789abcdef", 1)
	var want bytes.Buffer
	want.WriteString("WB-AAD\x00")
	want.WriteByte(1)
	writeLenPrefixed(&want, "WBEV1")
	writeLenPrefixed(&want, "sym-v1")
	var u32 [4]byte
	binary.BigEndian.PutUint32(u32[:], 1)
	want.Write(u32[:])
	writeLenPrefixed(&want, "0123456789abcdef")
	want.WriteByte(2)
	want.WriteByte(0)
	want.WriteByte(0)
	if !bytes.Equal(got, want.Bytes()) {
		t.Fatalf("AAD 帧不符:\n got %x\nwant %x", got, want.Bytes())
	}
}
