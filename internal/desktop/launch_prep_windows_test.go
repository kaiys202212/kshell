//go:build windows

package desktop

import "testing"

func TestSingleInstanceMutexNameMatchesWails(t *testing.T) {
	// Wails: id="wails-app-"+uniqueId; mutexName=id+"sim"；UniqueId=kshell-desktop
	if singleInstanceMutexName != "wails-app-kshell-desktopsim" {
		t.Fatalf("got %q", singleInstanceMutexName)
	}
}
