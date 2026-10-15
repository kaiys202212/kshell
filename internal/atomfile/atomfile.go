// Package atomfile 提供「同目录唯一临时文件 + Rename」的原子写辅助。
// 统一各处手写的 tmp := path + ".tmp" 写法。两点关键：
//  1. 唯一临时文件（CreateTemp）防「写一半崩溃留下半截文件」；
//  2. 包级互斥串行化——Windows 上并发 Rename 覆盖同一目标会 Access denied，
//     且这些落盘（配置/缓存/清单）本无并发必要。
package atomfile

import (
	"os"
	"path/filepath"
	"sync"
)

// writeMu 串行化所有 Write：防并发 Rename 覆盖同一目标互踩（Windows）。
var writeMu sync.Mutex

// Write 原子写 data 到 path：同目录 CreateTemp 唯一临时文件，成功后 Rename 覆盖目标。
// dirPerm 用于目标目录不存在时的创建。rename 成功后残留清理是空操作。
func Write(path string, data []byte, dirPerm os.FileMode) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return err
	}
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
