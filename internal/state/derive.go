package state

import "strings"

// deriveLinkState is the first match of the list in docs/api.md (Concept 11.6).
func deriveLinkState(l DlLink) LinkState {
	switch {
	case !l.Enabled:
		return LinkDisabled
	case strings.HasPrefix(l.ExtractionStatus, "ERROR"):
		return LinkExtractError
	case l.Finished:
		return LinkFinished
	case l.Skipped:
		return LinkSkipped
	case l.Running:
		return LinkRunning
	}
	return LinkWaiting
}
