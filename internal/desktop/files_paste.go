package desktop

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/yangk/kshell/internal/discovery"
)

// readClipboardFilesFn 读系统剪贴板文件列表：返回 (绝对路径列表, 是否剪切, err)。
// 平台文件 init 赋真实实现（Windows 同周期读 HDROP + Preferred DropEffect）；
// 定义为包级变量便于单测注入假剪贴板。注意：注入仅限无 t.Parallel 的测试使用。
var readClipboardFilesFn = func() ([]string, bool, error) {
	return nil, false, errors.New("err.clipboard.unsupported")
}

// PasteSummary 粘贴结果汇总，供前端 toast 展示。
type PasteSummary struct {
	Moved   int      `json:"Moved"`
	Copied  int      `json:"Copied"`
	Renamed []string `json:"Renamed"` // "a.txt → a (1).txt"
	Errors  []string `json:"Errors"`  // 首段为稳定 key 或原始错误文本
}

// PasteFiles 把来源项粘贴进工作区目录 dirRel，返回汇总。
// 来源二选一：treeRels 非空 → 树内剪贴板（move 由调用方决定）；
// 否则读系统剪贴板（资源管理器复制/剪切，move 由 Preferred DropEffect 决定）。
// 目标同名自动加后缀 "name (1)"，不覆盖已有文件。SSH 工作区不支持。
func (a *App) PasteFiles(wsPath, dirRel string, treeRels []string, move bool) (PasteSummary, error) {
	kind, local, _, err := a.parseWSRef(wsPath)
	if err != nil {
		return PasteSummary{}, err
	}
	if kind == discovery.KindSSH {
		return PasteSummary{}, errRemoteFSOpUnsupported
	}
	wsPath = local

	// showAll=true：超集树，被 ignore 的目标/来源也可粘贴
	tree, err := a.treeFor(wsPath, true)
	if err != nil {
		return PasteSummary{}, err
	}
	node, err := a.nodeAt(tree, wsPath, dirRel)
	if err != nil {
		return PasteSummary{}, err
	}
	if !node.IsDir {
		return PasteSummary{}, errNotDir
	}
	dstDir := node.Path

	type pasteItem struct {
		src  string // 绝对路径
		move bool
	}
	var items []pasteItem
	if len(treeRels) > 0 {
		for _, rel := range treeRels {
			rel = strings.TrimSpace(rel)
			if rel == "" {
				continue
			}
			abs := filepath.Join(wsPath, filepath.FromSlash(rel))
			if !underPath(wsPath, abs) || samePath(abs, wsPath) {
				return PasteSummary{}, errPathOutsideWorkspace
			}
			items = append(items, pasteItem{src: abs, move: move})
		}
	} else {
		files, cut, err := readClipboardFilesFn()
		if err != nil {
			return PasteSummary{}, err
		}
		for _, f := range files {
			if strings.TrimSpace(f) != "" {
				items = append(items, pasteItem{src: f, move: cut})
			}
		}
	}
	if len(items) == 0 {
		return PasteSummary{}, errClipboardFileListEmpty
	}

	sum := PasteSummary{}
	for _, it := range items {
		name, renamed := uniqueDestName(dstDir, filepath.Base(it.src))
		dst := filepath.Join(dstDir, name)
		if renamed {
			sum.Renamed = append(sum.Renamed, fmt.Sprintf("%s → %s", filepath.Base(it.src), name))
		}
		if it.move {
			// 移动目标不得位于源自身内部（underPath 含相等：把目录搬进自己/盖到自己都拒绝）
			if underPath(it.src, dstDir) {
				return sum, errMoveIntoSelf
			}
			if err := movePath(it.src, dst); err != nil {
				sum.Errors = append(sum.Errors, err.Error())
				continue
			}
			sum.Moved++
		} else {
			if err := copyAll(it.src, dst); err != nil {
				sum.Errors = append(sum.Errors, err.Error())
				_ = os.RemoveAll(dst) // 半成品不留现场
				continue
			}
			sum.Copied++
		}
	}

	a.invalidateTree(wsPath)
	return sum, nil
}

// uniqueDestName 在 dir 下为 name 找不冲突的名字：原名可用则原名，
// 否则 "stem (n).ext" 递增（n 从 1 起）。返回 (名字, 是否改名)。
func uniqueDestName(dir, name string) (string, bool) {
	if _, err := os.Lstat(filepath.Join(dir, name)); os.IsNotExist(err) {
		return name, false
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 1; ; n++ {
		cand := fmt.Sprintf("%s (%d)%s", stem, n, ext)
		if _, err := os.Lstat(filepath.Join(dir, cand)); os.IsNotExist(err) {
			return cand, true
		}
	}
}

// copyAll 递归复制 src（文件或目录）到 dst；dst 由调用方保证不冲突。
// 复制过程中跳过 dst 自身子树（把目录复制进自己内部时防无限递归）。
// 语义取舍：不保留 symlink（symlink 目标按普通文件物化，目录内文件 symlink 变普通副本；
// 指向目录的 symlink 复制会失败并中止该项，fail-safe）；目录权限固定 0o755，不保源权限位。
func copyAll(src, dst string) error {
	fi, err := os.Stat(src) // 跟随 symlink 取目标属性
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return copyFile(src, dst, fi.Mode())
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if underPath(dst, p) { // 含 p == dst（把目录复制进自己内部时防无限递归）
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(p, target, info.Mode())
	})
}

// copyFile 按源权限位复制单个文件内容。O_EXCL 保证不覆盖已存在文件：
// uniqueDestName 检查与写入之间的并发创建（TOCTOU）会以单项失败收场而非静默覆盖，
// 与「不覆盖已有内容」的确认弹窗承诺一致。
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// movePath 移动文件/目录：同卷优先 os.Rename；失败（跨卷/占用等）退化为
// 复制到新位置后删除源。任一环节失败返回错误（源保留）。
func movePath(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyAll(src, dst); err != nil {
		return err
	}
	// 复制完成后校验目标确已生成再删源，避免半复制丢数据
	if _, err := os.Lstat(dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}
