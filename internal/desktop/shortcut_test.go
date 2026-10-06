package desktop

import (
	"errors"
	"testing"
)

func TestPlanDesktopShortcut(t *testing.T) {
	cases := []struct {
		name     string
		ensured  bool
		exists   bool
		createOK bool
		create   bool
		mark     bool
	}{
		{name: "首次无快捷方式且创建成功", exists: false, createOK: true, create: true, mark: true},
		{name: "首次无快捷方式且创建失败", exists: false, createOK: false, create: true, mark: false},
		{name: "首次已有快捷方式", exists: true, create: false, mark: true},
		{name: "已标记且用户删除", ensured: true, exists: false, create: false, mark: false},
		{name: "已标记且仍在", ensured: true, exists: true, create: false, mark: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := planDesktopShortcut(tc.ensured, tc.exists, tc.createOK)
			if got.Create != tc.create || got.Mark != tc.mark {
				t.Fatalf("got %+v want create=%v mark=%v", got, tc.create, tc.mark)
			}
		})
	}
}

func TestEnsureDesktopShortcut_已存在只标记(t *testing.T) {
	var created, saved bool
	err := ensureDesktopShortcut(shortcutDeps{
		ensured: false,
		exists:  func() bool { return true },
		create: func() error {
			created = true
			return nil
		},
		mark: func() error {
			saved = true
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created || !saved {
		t.Fatalf("created=%v saved=%v", created, saved)
	}
}

func TestEnsureDesktopShortcut_首次创建后落盘(t *testing.T) {
	var created, saved bool
	err := ensureDesktopShortcut(shortcutDeps{
		ensured: false,
		exists:  func() bool { return false },
		create: func() error {
			created = true
			return nil
		},
		mark: func() error {
			saved = true
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created || !saved {
		t.Fatalf("created=%v saved=%v", created, saved)
	}
}

func TestEnsureDesktopShortcut_已标记则不创建(t *testing.T) {
	err := ensureDesktopShortcut(shortcutDeps{
		ensured: true,
		exists:  func() bool { return false },
		create: func() error {
			t.Fatal("不应创建")
			return nil
		},
		mark: func() error {
			t.Fatal("不应再标记")
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEnsureDesktopShortcut_创建失败不标记(t *testing.T) {
	var saved bool
	err := ensureDesktopShortcut(shortcutDeps{
		ensured: false,
		exists:  func() bool { return false },
		create:  func() error { return errors.New("create failed") },
		mark: func() error {
			saved = true
			return nil
		},
	})
	if err == nil {
		t.Fatal("期望创建失败")
	}
	if saved {
		t.Fatal("失败不应标记 ensured")
	}
}
