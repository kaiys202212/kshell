package clisync

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// ErrNotEmbedded 表示当前构建未内嵌 CLI 二进制（dev 构建只嵌了占位文件）。
var ErrNotEmbedded = errors.New("clisync: 当前构建未内嵌 CLI（dev 构建）")

// renameRetry 与 agentsync.writeAtomic 同口径：Windows sharing violation 的
// 重试次数与退避（1ms 递增，累计约 15ms）。
const renameRetry = 6

// releaseBinary 把二进制释放到 binDir/<BinaryName>。内容哈希相同跳过（current）；
// 不同则经 .new 临时文件 + rename 覆盖。Windows 上目标正被其它进程执行时
// rename 会 sharing violation——先把旧文件改名 .old（Windows 允许改名运行中的
// exe）再重试，成功后清理 .old；仍失败保留旧版返回错误，不损坏现场。
func releaseBinary(binDir string, binary []byte) (string, error) {
	if len(binary) == 0 {
		return stateNotEmbedded, ErrNotEmbedded
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return stateError, err
	}
	target := filepath.Join(binDir, BinaryName())
	if cur, err := os.ReadFile(target); err == nil && sha256Hex(cur) == sha256Hex(binary) {
		return stateCurrent, nil
	}

	tmp := target + ".new"
	if err := os.WriteFile(tmp, binary, 0o755); err != nil {
		return stateError, err
	}
	if err := renameWithFallback(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return stateError, err
	}
	// 清理历史遗留的 .old（本次覆盖成功后不再需要）。
	_ = os.Remove(target + ".old")
	return stateReleased, nil
}

// renameWithFallback 先直接 rename；Windows 下失败（旧副本运行中）则把目标
// 改名 .old 后重试。Unix 的 rename 本就可覆盖运行中二进制，fallback 不会触发。
func renameWithFallback(src, dst string) error {
	err := renameRetryLoop(src, dst)
	if err == nil || runtime.GOOS != "windows" {
		return err
	}
	// 目标可能正被运行：挪走旧文件再试一次。
	old := dst + ".old"
	_ = os.Remove(old)
	if err2 := renameRetryLoop(dst, old); err2 != nil {
		return err // 保留原始失败原因
	}
	return renameRetryLoop(src, dst)
}

// renameRetryLoop 带短退避重试 rename：覆盖并发写者（另一处同时释放）的窗口。
func renameRetryLoop(src, dst string) error {
	var err error
	for i := 0; i < renameRetry; i++ {
		if err = os.Rename(src, dst); err == nil {
			return nil
		}
		time.Sleep(time.Duration(i+1) * time.Millisecond)
	}
	return err
}
