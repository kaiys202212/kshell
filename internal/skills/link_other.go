//go:build !windows

package skills

import "errors"

func windowsJunction(_, _ string) error {
	return errors.New("junction unsupported")
}
