//go:build !windows

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

func spawnSelfDelayedImpl(exe string) error {
	pid := os.Getpid()
	script := fmt.Sprintf(
		"while kill -0 %d 2>/dev/null; do sleep 0.1; done; exec %s",
		pid, strconv.Quote(exe),
	)
	cmd := exec.Command("/bin/sh", "-c", script)
	return cmd.Start()
}
