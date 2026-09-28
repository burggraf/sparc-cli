//go:build localdemo

package database

import (
	"bytes"
	"context"
	"io"
	"strings"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/tools"
)

const maxTrialDiagnostic = 64 << 10

type trialDiagnostic struct{ bytes.Buffer }

func (d *trialDiagnostic) Write(p []byte) (int, error) {
	if remaining := maxTrialDiagnostic - d.Len(); remaining > 0 {
		_, _ = d.Buffer.Write(p[:min(len(p), remaining)])
	}
	return len(p), nil
}

// RestoreTrial retains the normal restore guards and returns only fixed failure
// categories. Raw PostgreSQL output is displayed only via explicit localOutput.
func RestoreTrial(ctx context.Context, request RestoreRequest, localOutput io.Writer) (stage, category string, err error) {
	return restoreTrialWith(ctx, request, defaultRecoveryOps(), localOutput)
}

// RestoreTrialContinue may partially write to a disposable target. PostgreSQL
// errors still cause failure; this is not a successful-recovery signal.
func RestoreTrialContinue(ctx context.Context, request RestoreRequest, localOutput io.Writer) (stage, category string, err error) {
	return restoreTrialContinueWith(ctx, request, defaultRecoveryOps(), localOutput)
}

func restoreTrialContinueWith(ctx context.Context, request RestoreRequest, ops recoveryOps, localOutput io.Writer) (stage, category string, err error) {
	if ops.run == nil {
		return "input validation", "", ErrRestore
	}
	run := ops.run
	ops.run = func(ctx context.Context, operation tools.RunRequest) (tools.RunResult, error) {
		operation.Mode = tools.ModeRestoreContinue
		return run(ctx, operation)
	}
	return restoreTrialWith(ctx, request, ops, localOutput)
}

func restoreTrialWith(ctx context.Context, request RestoreRequest, ops recoveryOps, localOutput io.Writer) (stage, category string, err error) {
	stage = "input validation"
	if ops.verify == nil || ops.prepareTool == nil || ops.observe == nil || ops.open == nil || ops.run == nil {
		return stage, "", ErrRestore
	}
	verify := ops.verify
	ops.verify = func(path, secret string) (archive.Manifest, error) {
		stage = "archive verification"
		return verify(path, secret)
	}
	prepare := ops.prepareTool
	ops.prepareTool = func(ctx context.Context, tool tools.Tool) error {
		stage = "client preparation"
		return prepare(ctx, tool)
	}
	observe := ops.observe
	ops.observe = func(ctx context.Context, params ConnectionParams, secret []byte, schemas []string) (CatalogObservation, error) {
		stage = "target preflight"
		return observe(ctx, params, secret, schemas)
	}
	open := ops.open
	ops.open = func(path string, component archive.Component, secret string) (io.ReadCloser, error) {
		stage = "archive decryption"
		return open(path, component, secret)
	}
	run := ops.run
	var diagnostic trialDiagnostic
	ops.run = func(ctx context.Context, request tools.RunRequest) (tools.RunResult, error) {
		stage = "pg_restore"
		request.Stderr = &diagnostic
		return run(ctx, request)
	}
	err = restoreWith(ctx, request, ops)
	if err != nil && stage == "pg_restore" {
		category = classifyTrialFailure(diagnostic.Bytes())
		if localOutput != nil {
			_, _ = localOutput.Write(diagnostic.Bytes())
		}
	}
	clear(diagnostic.Bytes())
	return stage, category, err
}

func classifyTrialFailure(stderr []byte) string {
	message := strings.ToLower(string(stderr))
	switch {
	case strings.Contains(message, "role ") && strings.Contains(message, "does not exist"):
		return "missing database role"
	case strings.Contains(message, "extension ") && (strings.Contains(message, "does not exist") || strings.Contains(message, "not available")):
		return "missing extension"
	case strings.Contains(message, "permission denied") || strings.Contains(message, "must be owner of"):
		return "permission denied"
	case strings.Contains(message, "violates foreign key constraint"):
		return "foreign key constraint violation"
	case strings.Contains(message, "duplicate key value"):
		return "duplicate key conflict"
	case strings.Contains(message, "already exists"):
		return "existing object conflict"
	case len(stderr) > 0:
		return "other PostgreSQL error"
	default:
		return "client stream or connection failure (details unavailable)"
	}
}
