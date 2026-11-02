package web

import (
	"io/fs"
	"testing"
)

// The embed works in a checkout without a build: the placeholder is there, so
// that `go build` does not fail on a missing directory.
func TestDistIsEmbeddedWithAPlaceholder(t *testing.T) {
	if _, err := fs.Stat(Dist(), ".gitkeep"); err != nil {
		t.Fatalf("placeholder missing: %v", err)
	}
}
