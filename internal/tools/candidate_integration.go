//go:build integration

package tools

import "context"

// RunCandidate is integration-only. It forces loopback routing for disposable
// local fixtures and never selects a production payload.
func RunCandidate(ctx context.Context, clientBin string, request RunRequest) (RunResult, error) {
	return runExternal(ctx, clientBin, request, "127.0.0.1")
}
