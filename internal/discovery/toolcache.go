package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/yangk/kshell/internal/providers"
)

// toolCacheVersion 是缓存文件结构版本，结构变更时递增，旧文件自然失效。
const toolCacheVersion = 1

// toolCacheEntry 记录一次版本探测对应的可执行文件指纹（mtime+size）与结果：
// CLI 升级/重装会改写文件指纹，从而自动失效并重新探测。
type toolCacheEntry struct {
	MTime   int64  `json:"mtime"`
	Size    int64  `json:"size"`
	Version string `json:"version"`
}

type toolCacheFile struct {
	Version int                       `json:"version"`
	Entries map[string]toolCacheEntry `json:"entries"`
}

// DetectAllCached 与 DetectAll 语义相同（含未安装项、已安装优先排序），但把版本
// 探测结果按可执行文件指纹缓存到 cachePath：文件没变就复用上次结果，避免每次启动
// 都为每个 CLI 拉一个 `<bin> --version` 子进程（启动耗时大头，也是多余的进程创建来源）。
// cachePath 为空时退化为 DetectAll（每次实探）。
func DetectAllCached(home string, ps []providers.Provider, cachePath string) []Tool {
	return detectAllCached(home, ps, cachePath, providers.ProbeVersion)
}

// detectAllCached 是 DetectAllCached 的主体，probeVersion 可注入以便测试。
func detectAllCached(home string, ps []providers.Provider, cachePath string, probeVersion func(bin string) string) []Tool {
	if cachePath == "" {
		return detectAll(home, ps, probeVersion)
	}

	prev := loadToolCache(cachePath)
	next := make(map[string]toolCacheEntry, len(prev))

	probe := func(bin string) string {
		if e, ok := prev[bin]; ok && fingerprintMatches(bin, e) {
			next[bin] = e // 文件未变：复用并保留条目
			return e.Version
		}
		v := probeVersion(bin)
		if v == "" || v == "unknown" {
			return v // 探测失败不落缓存，下次启动再试（避免把一次性失败固化）
		}
		if e, ok := fingerprintOf(bin); ok {
			e.Version = v
			next[bin] = e
		}
		return v
	}

	tools := detectAll(home, ps, probe)

	// 写失败不影响本次结果（下次启动再写），因此忽略错误
	if !sameCacheEntries(prev, next) {
		_ = saveToolCache(cachePath, next)
	}
	return tools
}

// fingerprintOf 取可执行文件的指纹；路径不存在或是目录时返回 false。
func fingerprintOf(path string) (toolCacheEntry, bool) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return toolCacheEntry{}, false
	}
	return toolCacheEntry{MTime: st.ModTime().UnixNano(), Size: st.Size()}, true
}

func fingerprintMatches(path string, e toolCacheEntry) bool {
	cur, ok := fingerprintOf(path)
	return ok && cur.MTime == e.MTime && cur.Size == e.Size
}

// loadToolCache 读缓存；缺失/损坏/版本不符一律当空缓存（不报错，最坏只是重探一次）。
func loadToolCache(path string) map[string]toolCacheEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]toolCacheEntry{}
	}
	var f toolCacheFile
	if err := json.Unmarshal(data, &f); err != nil || f.Version != toolCacheVersion || f.Entries == nil {
		return map[string]toolCacheEntry{}
	}
	return f.Entries
}

func saveToolCache(path string, entries map[string]toolCacheEntry) error {
	data, err := json.Marshal(toolCacheFile{Version: toolCacheVersion, Entries: entries})
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// 原子替换：避免写一半崩溃留下半截 JSON（下次启动按损坏缓存处理，但干净些更好）
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // rename 成功后残留清理是空操作
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func sameCacheEntries(a, b map[string]toolCacheEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || vb != va {
			return false
		}
	}
	return true
}
