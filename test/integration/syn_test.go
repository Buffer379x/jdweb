//go:build integration

package integration

import (
	"testing"

	"github.com/Buffer379x/jdweb/internal/state"
)

// T-SYN-01: start with a reachable JDownloader.
func TestSYN01_StartConnected(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	waitFor(t, "connected", 30e9, func() bool { return a.status(t).Connection == state.ConnConnected })
	st := a.status(t)
	if st.JD.Revision < 50000 || !st.JD.Docker {
		t.Errorf("jd info = %+v", st.JD)
	}
	if st.Controller != "IDLE" {
		t.Errorf("controller = %s", st.Controller)
	}
	_ = a.downloads(t)
	_ = a.grabber(t)
}
