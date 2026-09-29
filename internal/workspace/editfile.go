package workspace

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// maxEditBytes 是可编辑文本文件的大小上限：整文件读入内存，必须设顶。
const maxEditBytes = 1024 * 1024

var (
	// ErrFileTooLarge 文件超过编辑大小上限。
	ErrFileTooLarge = errors.New("文件超过 1MB，不支持编辑")
	// ErrBinaryFile 二进制文件不支持编辑。
	ErrBinaryFile = errors.New("二进制文件不支持编辑")
	// ErrIsDirectory 目录不支持编辑。
	ErrIsDirectory = errors.New("目录不支持编辑")
)

// EditContent 是编辑态需要的整文件内容：Text 一律以 \n 归一，
// 原行尾记录在 EOL（"lf"|"crlf"），保存时按 EOL 还原。
type EditContent struct {
	Text string
	EOL  string
	Size int64
}

// ReadForEdit 整读一个文本文件供编辑：超限报 ErrFileTooLarge，
// 二进制报 ErrBinaryFile；行尾按多数派探测（CRLF 占多按 crlf 处理）。
func ReadForEdit(path string) (EditContent, error) {
	info, err := os.Stat(path)
	if err != nil {
		return EditContent{}, err
	}
	if info.IsDir() {
		return EditContent{}, ErrIsDirectory
	}
	if info.Size() > maxEditBytes {
		return EditContent{}, ErrFileTooLarge
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return EditContent{}, err
	}
	if isBinary(data) {
		return EditContent{}, ErrBinaryFile
	}

	crlf := bytes.Count(data, []byte("\r\n"))
	lf := bytes.Count(data, []byte("\n")) - crlf
	eol := "lf"
	if crlf > lf {
		eol = "crlf"
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\r") // 孤立的旧 Mac 风格 \r 兜底清掉

	return EditContent{Text: text, EOL: eol, Size: info.Size()}, nil
}

// SaveEdit 把编辑后的文本写回 path：text 按 eol 还原行尾后经
// 「同目录临时文件 + os.Rename」落盘（保留原文件权限位）。
// 注意两点已知限制：①未做 fsync，进程崩溃/断电极端情况下新内容可能未真正落盘；
// ②Windows 上目标被其它进程占用（无 FILE_SHARE_DELETE）或带只读属性时
// rename 会失败——错误原样浮出给前端提示，不做重试。
func SaveEdit(path, text, eol string) error {
	if eol != "crlf" && eol != "lf" {
		eol = "lf"
	}
	// 先清 \r 再统一还原，防止往返累积
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if eol == "crlf" {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	if int64(len(text)) > maxEditBytes {
		return ErrFileTooLarge
	}

	// 目标可能尚不存在（新建文件保存）：有旧文件则保留权限位，没有按 0644
	var mode os.FileMode = 0o644
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return ErrIsDirectory
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".kshell-edit-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}
