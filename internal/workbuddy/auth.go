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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Credential 是从 WorkBuddy 桌面端凭据文件解出的一次性账号凭据。
// 只存活在 kshell 进程内，绝不落盘、不入 config.yaml。
type Credential struct {
	UID          string
	AccessToken  string
	RefreshToken string
	Domain       string // 登录域，如 www.codebuddy.cn；空视为国内区
	ExpiresAtMs  int64  // accessToken 过期时间（毫秒）；0=未知
}

// Region 返回凭据所属区：国内区按域名判断，其余一律视为 global。
func (c Credential) Region() string {
	switch c.Domain {
	case "", "www.codebuddy.cn", "www.workbuddy.cn", "wb.tencentbuddy.com", "copilot.tencent.com":
		return "cn"
	default:
		return "global"
	}
}

// expired 判断 accessToken 是否已确定过期（未知时间不算过期）。
func (c Credential) expired(now time.Time) bool {
	return c.ExpiresAtMs > 0 && now.UnixMilli() >= c.ExpiresAtMs
}

// encryptedField 是桌面端敏感字段的加密包装（Windows 版本把 token 存成这种形状）。
type encryptedField struct {
	Encrypted int    `json:"$wbEncrypted"`
	Envelope  string `json:"envelope"`
}

// isEncryptedFieldWrapper 判断 JSON 值是否为加密包装。
func isEncryptedFieldWrapper(raw json.RawMessage) bool {
	if len(raw) == 0 || raw[0] != '{' {
		return false
	}
	var f encryptedField
	return json.Unmarshal(raw, &f) == nil && f.Encrypted == 1 && f.Envelope != ""
}

// aadDomain 与帧常量照抄桌面端/dsh 插件的对称加密帧，见 fieldAad。
var aadDomain = []byte("WB-AAD\x00")

const (
	aadFormatField = "WBEV1"
	aadScheme      = "sym-v1"
	aadFramingCode = 2 // field
)

func writeUint32(b *bytes.Buffer, v uint32) {
	var u [4]byte
	binary.BigEndian.PutUint32(u[:], v)
	b.Write(u[:])
}

func writeLenPrefixed(b *bytes.Buffer, s string) {
	writeUint32(b, uint32(len(s)))
	b.WriteString(s)
}

// fieldAad 构造字段级信封的 AES-GCM 附加认证数据：
// "WB-AAD\0" + 0x01 + lenpref("WBEV1") + lenpref("sym-v1") + uint32(suite)
// + lenpref(keyId) + 0x02 + 0x00 + 0x00。
func fieldAad(keyID string, suite int) []byte {
	var b bytes.Buffer
	b.Write(aadDomain)
	b.WriteByte(1)
	writeLenPrefixed(&b, aadFormatField)
	writeLenPrefixed(&b, aadScheme)
	writeUint32(&b, uint32(suite))
	writeLenPrefixed(&b, keyID)
	b.WriteByte(aadFramingCode)
	b.WriteByte(0)
	b.WriteByte(0)
	return b.Bytes()
}

// openEncryptedField 解开一个加密字段：envelope base64→JSON，
// keyId 必须等于 hex(sha256(key))[:16]，AES-256-GCM（12 字节 nonce + 16 字节 tag）。
func openEncryptedField(f encryptedField, key []byte) (string, error) {
	if len(key) != 32 {
		return "", errors.New("err.workbuddy.bad_key")
	}
	raw, err := base64.StdEncoding.DecodeString(f.Envelope)
	if err != nil {
		return "", fmt.Errorf("err.workbuddy.bad_envelope|%v", err)
	}
	var env struct {
		Suite      int    `json:"suite"`
		KeyID      string `json:"keyId"`
		Nonce      string `json:"nonce"`
		AuthTag    string `json:"authTag"`
		Ciphertext string `json:"ciphertext"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("err.workbuddy.bad_envelope|%v", err)
	}
	sum := sha256.Sum256(key)
	if env.KeyID != hex.EncodeToString(sum[:])[:16] {
		return "", errors.New("err.workbuddy.key_mismatch")
	}
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil {
		return "", fmt.Errorf("err.workbuddy.bad_envelope|%v", err)
	}
	tag, err := base64.StdEncoding.DecodeString(env.AuthTag)
	if err != nil {
		return "", fmt.Errorf("err.workbuddy.bad_envelope|%v", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("err.workbuddy.bad_envelope|%v", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, len(nonce))
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, nonce, append(ciphertext, tag...), fieldAad(env.KeyID, env.Suite))
	if err != nil {
		return "", fmt.Errorf("err.workbuddy.decrypt_failed|%v", err)
	}
	return string(plain), nil
}

// credentialField 解析一个可能加密的字符串字段：明文字符串直接用；
// 加密包装用 key 解开；其他形状视为缺失。
func credentialField(raw json.RawMessage, key []byte) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", err
		}
		return s, nil
	}
	if isEncryptedFieldWrapper(raw) {
		var f encryptedField
		if err := json.Unmarshal(raw, &f); err != nil {
			return "", err
		}
		return openEncryptedField(f, key)
	}
	return "", nil
}

// expiryToMs 归一时间戳：大于 1e12 视为毫秒，否则按秒乘 1000。
func expiryToMs(v int64) int64 {
	if v <= 0 {
		return 0
	}
	if v > 0xe8d4a51000 {
		return v
	}
	return v * 1000
}

// parseAuthDocument 解析 .info 凭据文档（嵌套 {"auth","account"} 与扁平两种形态）。
// 无 accessToken 即报错；token 解密失败同样报错（宁缺勿滥，不能拿半截 token 用）。
func parseAuthDocument(data []byte, key []byte) (Credential, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return Credential{}, fmt.Errorf("err.workbuddy.bad_auth_file|%v", err)
	}
	auth, identity := json.RawMessage(data), json.RawMessage(data)
	if a, ok := doc["auth"]; ok && len(a) > 0 && a[0] == '{' {
		auth = a
		if acc, ok := doc["account"]; ok {
			identity = acc
		}
	}
	var authFields, identityFields map[string]json.RawMessage
	if err := json.Unmarshal(auth, &authFields); err != nil {
		return Credential{}, fmt.Errorf("err.workbuddy.bad_auth_file|%v", err)
	}
	if err := json.Unmarshal(identity, &identityFields); err != nil {
		return Credential{}, fmt.Errorf("err.workbuddy.bad_auth_file|%v", err)
	}
	token, err := credentialField(authFields["accessToken"], key)
	if err != nil {
		return Credential{}, err
	}
	if token == "" {
		return Credential{}, errors.New("err.workbuddy.no_token")
	}
	refresh, _ := credentialField(authFields["refreshToken"], key)
	domain, _ := credentialField(authFields["domain"], key)
	uid, _ := credentialField(identityFields["uid"], key)
	cred := Credential{
		AccessToken:  token,
		RefreshToken: refresh,
		Domain:       domain,
		UID:          uid,
	}
	if raw, ok := authFields["expiresAt"]; ok {
		var v int64
		if json.Unmarshal(raw, &v) == nil {
			cred.ExpiresAtMs = expiryToMs(v)
		}
	}
	return cred, nil
}

// authFilePaths 返回凭据文件候选（国内区在前，国际区随后；LOCALAPPDATA 优先于 APPDATA）。
func authFilePaths(localAppData, roamingAppData string) []string {
	names := []string{"workbuddy-desktop.info", "workbuddy-desktop-ai.info"}
	var out []string
	for _, base := range []string{localAppData, roamingAppData} {
		if base == "" {
			continue
		}
		dir := filepath.Join(base, "CodeBuddyExtension", "Data", "Public", "auth")
		for _, n := range names {
			out = append(out, filepath.Join(dir, n))
		}
	}
	return out
}

// extractAtRestKey 借 WorkBuddy 桌面端自身导出 at-rest key payload，
// 再取 atRestSecretKey 字符串（按其 UTF-8 字节，非解码值）的 sha256 作 32 字节密钥。
func extractAtRestKey(exe string) ([]byte, error) {
	payload, err := atRestKeyRunner(exe, atRestKeyScript)
	if err != nil {
		return nil, fmt.Errorf("err.workbuddy.key_unavailable|%v", err)
	}
	var p struct {
		AtRestSecretKey string `json:"atRestSecretKey"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(payload), &p); err != nil || p.AtRestSecretKey == "" {
		return nil, errors.New("err.workbuddy.key_unavailable|payload 缺 atRestSecretKey")
	}
	sum := sha256.Sum256([]byte(p.AtRestSecretKey))
	return sum[:], nil
}

// loadCredentialFrom 依次尝试候选凭据文件：读到文档后借 exe 提 key 解密。
// 文件全缺失报 err.workbuddy.not_installed。
func loadCredentialFrom(paths []string, exe string) (Credential, error) {
	var data []byte
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if len(bytes.TrimSpace(b)) > 0 {
			data = b
			break
		}
	}
	if data == nil {
		return Credential{}, errors.New("err.workbuddy.not_installed")
	}
	key, err := extractAtRestKey(exe)
	if err != nil {
		return Credential{}, err
	}
	return parseAuthDocument(data, key)
}

// LoadCredential 定位并解密当前登录凭据（单账号，取文件顶层登录态）。
func LoadCredential() (Credential, error) {
	exe, ok := FindAppExecutable()
	if !ok {
		return Credential{}, errors.New("err.workbuddy.exe_not_found")
	}
	return loadCredentialFrom(
		authFilePaths(os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA")),
		exe,
	)
}
