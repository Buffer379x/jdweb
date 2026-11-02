package state

import (
	"strings"

	"github.com/Buffer379x/jdweb/internal/jd"
)

// deriveLinkState is the first match of the list in docs/api.md (Concept 11.6).
// failed is a final state of JDownloader that is not a success (an entry of
// advancedStatus.FinalLinkState); the text of status is never looked at.
func deriveLinkState(l DlLink) LinkState {
	switch {
	case !l.Enabled:
		return LinkDisabled
	case strings.HasPrefix(l.ExtractionStatus, "ERROR"):
		return LinkExtractError
	case l.FinalState != "" && !jd.FinalLinkStateFinished(l.FinalState):
		return LinkFailed
	case l.Finished:
		return LinkFinished
	case l.Skipped:
		return LinkSkipped
	case l.Running:
		return LinkRunning
	}
	return LinkWaiting
}
