package workbuddy

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"time"
)

// atRestKeyScript 借 WorkBuddy 桌面端自身（ELECTRON_RUN_AS_NODE 模式）导出
// at-rest key payload——密钥不存在于任何静态文件，只能由 app 二进制提供。
// 脚本与 dsh-connect-workbuddy 一致，输出 stdout（payload JSON）。
const atRestKeyScript = `try{process.stdout.write(process._linkedBinding('electron_browser_workbuddy_storage').loggerGet())}catch(e){process.exitCode=3;process.stderr.write(String(e&&e.message||e))}`

// atRestKeyRunner 是 key 提取的进程执行钩子，测试可注入。返回 stdout（payload JSON）。
var atRestKeyRunner = runAtRestKeyProcess

const atRestKeyTimeout = 5 * time.Second

// runAtRestKeyProcess 执行提取进程：超时兜底杀进程，隐藏窗口避免闪框。
func runAtRestKeyProcess(exe, script string) ([]byte, error) {
	cmd := exec.Command(exe, "-e", script)
	cmd.Env = append(os.Environ(), "ELECTRON_RUN_AS_NODE=1")
	hideWindow(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(atRestKeyTimeout)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			if stderr.Len() > 0 {
				return nil, errors.New(stderr.String())
			}
			return nil, err
		}
		return stdout.Bytes(), nil
	case <-timer.C:
		_ = cmd.Process.Kill()
		<-done
		return nil, errors.New("workbuddy at-rest key fetch timeout")
	}
}
