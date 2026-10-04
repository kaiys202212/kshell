package version

import "testing"

func TestCurrentDefaultsToDev(t *testing.T) {
	if Current() != "dev" {
		t.Fatalf("Current() = %q, want dev", Current())
	}
}
