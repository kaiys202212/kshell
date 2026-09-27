package remote

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxScanFiles  = 5000
	maxScanFileKB = 2 * 1024
)

// Candidate 是扫描器从工程文件里挖出来的连接候选，必须经用户确认才写入 Store。
type Candidate struct {
	Name         string
	Host         string
	User         string
	Port         int
	IdentityFile string
	Confidence   string // high | medium | low
	Source       string // sshconfig | env | spring | deploy | docs
	SourceFile   string
	SourceLine   int
}

func (c Candidate) Target() string {
	if c.User == "" {
		return c.Host
	}
	return c.User + "@" + c.Host
}

type Scanner interface {
	ID() string
	Match(relPath string) bool
	Extract(root, absPath string) ([]Candidate, error)
}

var confidenceRank = map[string]int{"high": 3, "medium": 2, "low": 1}

// ScanWorkspace 遍历工作区文件，调用启用的扫描器收集候选；只读，绝不写文件。
func ScanWorkspace(root string, scanners []Scanner, enabled map[string]bool, exclude []string) ([]Candidate, error) {
	skip := map[string]bool{}
	for _, name := range exclude {
		skip[name] = true
	}

	var found []Candidate
	count := 0

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if skip[d.Name()] {
			return nil
		}

		count++
		if count > maxScanFiles {
			return fs.SkipAll
		}

		info, err := d.Info()
		if err != nil || info.Size() > maxScanFileKB*1024 {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		for _, s := range scanners {
			if enabled != nil {
				if on, ok := enabled[s.ID()]; ok && !on {
					continue
				}
			}
			if !s.Match(rel) {
				continue
			}
			cands, err := s.Extract(root, path)
			if err != nil {
				continue // 单个文件解析失败不影响整体
			}
			found = append(found, cands...)
		}
		return nil
	})
	if err != nil {
		return found, err
	}

	return Dedupe(found), nil
}

// Dedupe 按 host+user+port 折叠，保留置信度最高的那条（并记下来源文件）。
func Dedupe(cands []Candidate) []Candidate {
	type key struct {
		host, user string
		port       int
	}

	best := map[key]Candidate{}
	for _, c := range cands {
		if c.Host == "" {
			continue
		}
		k := key{strings.ToLower(c.Host), strings.ToLower(c.User), c.Port}
		if prev, ok := best[k]; ok && confidenceRank[prev.Confidence] >= confidenceRank[c.Confidence] {
			continue
		}
		best[k] = c
	}

	out := make([]Candidate, 0, len(best))
	for _, c := range best {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if confidenceRank[out[i].Confidence] != confidenceRank[out[j].Confidence] {
			return confidenceRank[out[i].Confidence] > confidenceRank[out[j].Confidence]
		}
		return out[i].Host < out[j].Host
	})
	return out
}

// IsRegularFile 供扫描器判断文件是否可读。
func IsRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
