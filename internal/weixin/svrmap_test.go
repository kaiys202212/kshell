package weixin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSvrMapSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "weixin-svr-map.json")
	m := map[string]svrAttr{
		"9001": {ChatID: "c1", SessionID: "s1", Title: "会话A", Ts: time.Now().Unix()},
		"9002": {ChatID: "c2", SessionID: "s2", Title: "会话B", Ts: time.Now().Unix()},
	}
	order := []string{"9001", "9002"}
	if err := saveSvrMapAccount(path, "wx-u1", m, order); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, gotOrder := loadSvrMapAccount(path, "wx-u1")
	if len(got) != 2 || got["9001"].ChatID != "c1" || got["9001"].SessionID != "s1" {
		t.Fatalf("load = %+v", got)
	}
	if len(gotOrder) != 2 || gotOrder[0] != "9001" {
		t.Fatalf("order = %v", gotOrder)
	}
	// 另一 wxid 隔离
	other, _ := loadSvrMapAccount(path, "wx-other")
	if len(other) != 0 {
		t.Fatalf("其它账号应空: %+v", other)
	}
}

func TestSvrMapCorruptBecomesEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "weixin-svr-map.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ := loadSvrMapAccount(path, "u1")
	if len(got) != 0 {
		t.Fatalf("损坏应空表: %+v", got)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("应留下 .bak: %v", err)
	}
}

func TestSvrMapTTLPrune(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "weixin-svr-map.json")
	old := time.Now().Add(-svrMapTTL - time.Hour).Unix()
	fresh := time.Now().Unix()
	f := svrMapFile{
		Version: 1,
		Accounts: map[string][]svrMapFileEntry{
			"u1": {
				{SvrID: "old", ChatID: "c1", Ts: old},
				{SvrID: "new", ChatID: "c2", SessionID: "s2", Ts: fresh},
			},
		},
	}
	raw, _ := json.Marshal(f)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, order := loadSvrMapAccount(path, "u1")
	if len(got) != 1 || got["new"].ChatID != "c2" {
		t.Fatalf("过期应剔除: %+v", got)
	}
	if len(order) != 1 || order[0] != "new" {
		t.Fatalf("order = %v", order)
	}
}

func TestSvrMapEmptyWxidNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.json")
	m := map[string]svrAttr{"1": {ChatID: "c1", Ts: 1}}
	if err := saveSvrMapAccount(path, "", m, []string{"1"}); err != nil {
		t.Fatalf("空 wxid save 应 no-op: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("空 wxid 不应写文件")
	}
}
