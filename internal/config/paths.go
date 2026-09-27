package config

import (
	"os"
	"path/filepath"
)

const rootDirName = ".kshell"

// Layout 描述 ~/.kshell 下的全部持久化位置，集中一处便于测试与后续扩展。
type Layout struct {
	Root        string
	Config      string
	Connections string
	Providers   string
	Cache       string
	CacheIndex  string
}

func Paths() (Layout, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Layout{}, err
	}
	root := filepath.Join(home, rootDirName)
	cache := filepath.Join(root, "cache")

	return Layout{
		Root:        root,
		Config:      filepath.Join(root, "config.yaml"),
		Connections: filepath.Join(root, "connections.yaml"),
		Providers:   filepath.Join(root, "providers.yaml"),
		Cache:       cache,
		CacheIndex:  filepath.Join(cache, "index.json"),
	}, nil
}

func EnsureRoot(p Layout) error {
	return os.MkdirAll(p.Cache, 0o755)
}
