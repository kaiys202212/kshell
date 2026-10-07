//go:build windows

package desktop

import (
	"bytes"
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	cfUnicodeText = 13
	cfHDROP       = 15
	cfDIB         = 8
)

var (
	modUser32   = windows.NewLazySystemDLL("user32.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")
	modShell32  = windows.NewLazySystemDLL("shell32.dll")

	procOpenClipboard              = modUser32.NewProc("OpenClipboard")
	procCloseClipboard             = modUser32.NewProc("CloseClipboard")
	procIsClipboardFormatAvailable = modUser32.NewProc("IsClipboardFormatAvailable")
	procGetClipboardData           = modUser32.NewProc("GetClipboardData")
	procRegisterClipboardFormatW   = modUser32.NewProc("RegisterClipboardFormatW")
	procGlobalLock                 = modKernel32.NewProc("GlobalLock")
	procGlobalUnlock               = modKernel32.NewProc("GlobalUnlock")
	procGlobalSize                 = modKernel32.NewProc("GlobalSize")
	procDragQueryFileW             = modShell32.NewProc("DragQueryFileW")
)

func readClipboardSnapshot() (clipboardSnapshot, error) {
	if err := openClipboardRetry(); err != nil {
		return clipboardSnapshot{}, err
	}
	defer procCloseClipboard.Call()

	var snap clipboardSnapshot
	if files, err := clipboardFiles(); err == nil {
		snap.Files = files
	}
	if text, err := clipboardUnicodeText(); err == nil {
		snap.Text = text
	}
	if png, err := clipboardPNG(); err == nil {
		snap.PNG = png
	}
	return snap, nil
}

func openClipboardRetry() error {
	var last error
	for i := 0; i < 8; i++ {
		r, _, err := procOpenClipboard.Call(0)
		if r != 0 {
			return nil
		}
		last = err
		time.Sleep(8 * time.Millisecond)
	}
	if last == nil {
		// 普通英文诊断串而非 key：本值只会作为 %w 参数塞进外层 key 的 {{0}}，
		// 前端不翻译参数，若用 key 会在界面上露出裸 key。
		last = errors.New("OpenClipboard failed")
	}
	return fmt.Errorf("err.clipboard.open_failed|%w", last)
}

func formatAvailable(format uint32) bool {
	r, _, _ := procIsClipboardFormatAvailable.Call(uintptr(format))
	return r != 0
}

func clipboardUnicodeText() (string, error) {
	if !formatAvailable(cfUnicodeText) {
		return "", fmt.Errorf("err.clipboard.no_text")
	}
	data, err := clipboardBytes(cfUnicodeText)
	if err != nil {
		return "", err
	}
	u16 := bytesToUint16(data)
	return syscall.UTF16ToString(u16), nil
}

func clipboardFiles() ([]string, error) {
	if !formatAvailable(cfHDROP) {
		return nil, fmt.Errorf("err.clipboard.no_files")
	}
	h, _, err := procGetClipboardData.Call(cfHDROP)
	if h == 0 {
		return nil, fmt.Errorf("err.clipboard.read_file_list_failed|%w", callErr(err))
	}
	n, _, _ := procDragQueryFileW.Call(h, uintptr(^uint32(0)), 0, 0)
	if n == 0 {
		return nil, fmt.Errorf("err.clipboard.file_list_empty")
	}
	files := make([]string, 0, n)
	for i := uintptr(0); i < n; i++ {
		chars, _, _ := procDragQueryFileW.Call(h, i, 0, 0)
		buf := make([]uint16, chars+1)
		procDragQueryFileW.Call(h, i, uintptr(unsafe.Pointer(&buf[0])), chars+1)
		if p := syscall.UTF16ToString(buf); p != "" {
			files = append(files, p)
		}
	}
	return files, nil
}

func clipboardPNG() ([]byte, error) {
	pngFmtName, err := windows.UTF16PtrFromString("PNG")
	if err != nil {
		return nil, err
	}
	pngFmt, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(pngFmtName)))
	if pngFmt != 0 && formatAvailable(uint32(pngFmt)) {
		if b, err := clipboardBytes(uint32(pngFmt)); err == nil && isPNG(b) {
			return b, nil
		}
	}
	if !formatAvailable(cfDIB) {
		return nil, fmt.Errorf("err.clipboard.no_image")
	}
	dib, err := clipboardBytes(cfDIB)
	if err != nil {
		return nil, err
	}
	return dibToPNG(dib)
}

func clipboardBytes(format uint32) ([]byte, error) {
	h, _, err := procGetClipboardData.Call(uintptr(format))
	if h == 0 {
		return nil, fmt.Errorf("err.clipboard.get_data_failed|%w", callErr(err))
	}
	ptr, _, err := procGlobalLock.Call(h)
	if ptr == 0 {
		return nil, fmt.Errorf("err.clipboard.global_lock_failed|%w", callErr(err))
	}
	defer procGlobalUnlock.Call(h)
	size, _, _ := procGlobalSize.Call(h)
	if size == 0 {
		return nil, fmt.Errorf("err.clipboard.data_empty")
	}
	return copyLocked(ptr, int(size)), nil
}

// callErr 规整 LazyProc.Call 的错误：失败但 last error 为 0（errnoErr→nil）时用固定
// 英文串兜底，保证 %w 总能带上非 nil 参数；该值只作诊断参数，不直接面向界面。
func callErr(err error) error {
	if err == nil {
		return errors.New("Win32 call failed")
	}
	return err
}

// copyLocked 从 GlobalLock 得到的 uintptr 拷出一份，避免 uintptr→unsafe.Pointer 触发 vet。
func copyLocked(ptr uintptr, size int) []byte {
	var sl struct {
		data uintptr
		len  int
		cap  int
	}
	sl.data = ptr
	sl.len = size
	sl.cap = size
	src := *(*[]byte)(unsafe.Pointer(&sl))
	out := make([]byte, size)
	copy(out, src)
	return out
}

func bytesToUint16(b []byte) []uint16 {
	if len(b) < 2 {
		return nil
	}
	n := len(b) / 2
	out := make([]uint16, n)
	for i := 0; i < n; i++ {
		out[i] = uint16(b[i*2]) | uint16(b[i*2+1])<<8
	}
	return out
}

func isPNG(b []byte) bool {
	return bytes.HasPrefix(b, []byte{0x89, 'P', 'N', 'G'})
}
