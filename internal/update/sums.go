package update

import (
	"fmt"
	"strings"
)

// ParseSHA256SUMS 从 sha256sum 文本里取出 filename 对应的小写十六进制哈希。
func ParseSHA256SUMS(data []byte, filename string) (string, error) {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 支持 "HASH  name" 或 "HASH *name"
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if name != filename {
			continue
		}
		sum := strings.ToLower(fields[0])
		if len(sum) != 64 {
			return "", fmt.Errorf("err.update.sums_length_invalid|%s", filename)
		}
		return sum, nil
	}
	return "", fmt.Errorf("err.update.sums_missing_file|%s", filename)
}
