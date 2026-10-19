package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLastSeenVersionRoundTrip(t *testing.T) {
	p := tempPaths(t)
	cfg := Default()
	if cfg.LastSeenVersion != "" {
		t.Fatal("默认不应已记录版本")
	}
	cfg.LastSeenVersion = "v1.2.3"
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LastSeenVersion != "v1.2.3" {
		t.Fatalf("应保留 last_seen_version, got %q", loaded.LastSeenVersion)
	}
}

func TestLastSeenVersionYAMLKey(t *testing.T) {
	data, err := yaml.Marshal(Config{LastSeenVersion: "v9.9.9"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "last_seen_version:") {
		t.Fatalf("yaml 应含 last_seen_version 键: %s", data)
	}
	var c Config
	if err := yaml.Unmarshal([]byte("last_seen_version: v0.1.0\n"), &c); err != nil {
		t.Fatal(err)
	}
	if c.LastSeenVersion != "v0.1.0" {
		t.Fatalf("反序列化 last_seen_version 失败, got %q", c.LastSeenVersion)
	}
}

func TestLastSeenVersionNormalizedTrims(t *testing.T) {
	c := Config{LastSeenVersion: "  v1.0.0  "}.normalized()
	if c.LastSeenVersion != "v1.0.0" {
		t.Fatalf("normalized 应去首尾空白, got %q", c.LastSeenVersion)
	}
}
