package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadChatAttachmentData 读取聊天附件缓存文件供前端弹窗预览。
// 仅允许 attachmentsRoot/<chatID>/ 下的路径：Clean 后做根前缀校验 + 至少两级
//（chatID/文件名），且清洗前后首段（chatID）必须一致——../ 换目录即使仍在
// 缓存根内也拒绝，防止穿越；缓存目录外路径一律拒绝，避免成为任意读文件原语。
// 根目录为空表示附件落盘未装配，直接报 errNotReady。
func (a *App) ReadChatAttachmentData(absPath string) (FileBytes, error) {
	root := a.snapshot().ChatAttachmentsDir
	if root == "" {
		return FileBytes{}, errNotReady
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return FileBytes{}, err
	}
	rawRel, ok := relUnderRoot(rootAbs, absPath)
	if !ok {
		return FileBytes{}, errPathOutsideWorkspace
	}
	// 原始路径首段即归属 chatID：清洗后必须仍落在该 chatID 目录内
	rawFirst, _, _ := strings.Cut(rawRel, "/")
	if rawFirst == "" || rawFirst == ".." {
		return FileBytes{}, errPathOutsideWorkspace
	}
	cleaned := filepath.Clean(absPath)
	cleanRel, ok := relUnderRoot(rootAbs, cleaned)
	if !ok {
		return FileBytes{}, errPathOutsideWorkspace
	}
	first, rest, _ := strings.Cut(cleanRel, "/")
	if first != rawFirst || rest == "" { // 至少两级：<chatID>/<文件名>
		return FileBytes{}, errPathOutsideWorkspace
	}
	return readFileBytesLimited(cleaned, maxPreviewBytes)
}

// relUnderRoot 判断 p（按 / 归一）是否位于 root 之下，返回其相对路径；
// 不在根内或已是归一化外的 .. 逃逸时返回 false。
func relUnderRoot(root, p string) (string, bool) {
	prefix := filepath.ToSlash(root)
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	rest, found := strings.CutPrefix(filepath.ToSlash(p), prefix)
	if !found || rest == "" || rest == ".." || strings.HasPrefix(rest, "../") {
		return "", false
	}
	return rest, true
}

// OpenLocalPath 用系统默认程序打开任意本地文件：聊天附件引用的工作区外路径用
//（工作区内文件走 OpenInDefaultApp，带 resolveWorkspaceFile 校验）。
// 不限制路径范围：agent 本就能在工作区内写任意文件，信任级别一致，加白名单
// 只挡住合法用途（如打开 agent 生成的报告），挡不住已获信任的 agent。
func (a *App) OpenLocalPath(absPath string) error {
	fi, err := os.Stat(absPath)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return fmt.Errorf("err.files.dir_not_openable")
	}
	return openLocalFile(absPath)
}
