//go:build !windows

package update

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

func spawnReplaceDefault(currentExe, newExe string) error {
	script := fmt.Sprintf(
		"while kill -0 %d 2>/dev/null; do sleep 0.1; done; mv %s %s && exec %s",
		os.Getpid(), strconv.Quote(newExe), strconv.Quote(currentExe), strconv.Quote(currentExe),
	)
	cmd := exec.Command("/bin/sh", "-c", script)
	return cmd.Start()
}
