package providers

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// yamlUnmarshalHelper 从 providers.yaml 文本里取第一项定义，供测试装配 Generic。
func yamlUnmarshalHelper(data string, out *GenericSpec) error {
	var file genericSpecFile
	if err := yaml.Unmarshal([]byte(data), &file); err != nil {
		return err
	}
	if len(file.Providers) == 0 {
		return errors.New("no providers declared")
	}
	*out = file.Providers[0]
	return nil
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func containsString(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
