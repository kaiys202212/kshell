package agentsync

import (
	"testing"

	"github.com/yangk/kshell/internal/config"
)

func TestInputFromModelCarriesDefaultPlatform(t *testing.T) {
	in := InputFromModel(config.ModelConfig{Enabled: true, DefaultPlatform: "p1"})
	if in.DefaultPlatformID != "p1" {
		t.Fatalf("DefaultPlatformID = %q, want p1", in.DefaultPlatformID)
	}
}

func TestInputFromModelCarriesActiveModel(t *testing.T) {
	in := InputFromModel(config.ModelConfig{Enabled: true, ActiveModel: "glm-5.3-flash"})
	if in.ActiveModel != "glm-5.3-flash" {
		t.Fatalf("ActiveModel = %q", in.ActiveModel)
	}
}
