package verify

import (
	"errors"

	"github.com/burggraf/sparc-cli/internal/archive"
)

var ErrOffline = errors.New("offline archive verification failed")

// Report exposes byte integrity separately from the archive's declared capture status.
type Report struct {
	IntegrityPassed bool
	CaptureStatus   string
	ComponentCount  int
}

// Offline verifies encrypted bytes and manifest claims without source config or
// network access. It does not determine whether a project was fully captured.
func Offline(dir, passphrase string) (Report, error) {
	manifest, err := archive.Verify(dir, passphrase)
	if err != nil {
		return Report{}, ErrOffline
	}
	captureStatus := "complete"
	for _, component := range manifest.Components {
		if component.Status != "complete" {
			captureStatus = "incomplete"
			break
		}
	}
	return Report{IntegrityPassed: true, CaptureStatus: captureStatus, ComponentCount: len(manifest.Components)}, nil
}
