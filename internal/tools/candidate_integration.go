//go:build integration

package tools

import "context"

// RunCandidate is integration-only. It uses the compiled-in client and forces
// loopback routing for disposable local fixtures.
func RunCandidate(ctx context.Context, request RunRequest) (RunResult, error) {
	if ctx == nil || request.testHostAddr != "" || !validRunRequest(request) {
		return RunResult{}, ErrRun
	}
	manifest, packagePath, cacheRoot, err := prepareProductionPayload(ctx, request.Tool)
	if err != nil {
		return RunResult{}, err
	}
	request.testHostAddr = "127.0.0.1"
	return runWith(ctx, request, manifest, packagePath, cacheRoot, defaultRunOps())
}
