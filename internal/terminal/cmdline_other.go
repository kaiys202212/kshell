//go:build !windows

package terminal

import "github.com/aymanbagabas/go-pty"

func applyCmdLine(*pty.Cmd, string) {}
