//go:build localdemo

package database

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/tools"
)

func TestRestoreTrialClassifiesBoundedPostgresFailure(t *testing.T) {
	ops := recoveryTestOps()
	ops.observe = func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
		return emptyRecoveryObservation(), nil
	}
	ops.open = func(string, archive.Component, string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("synthetic encrypted dump")), nil
	}
	ops.run = func(_ context.Context, request tools.RunRequest) (tools.RunResult, error) {
		if request.Stderr == nil {
			t.Fatal("diagnostic sink missing")
		}
		_, _ = request.Stderr.Write([]byte("pg_restore: error: role secret-user does not exist\n"))
		return tools.RunResult{}, tools.ErrRun
	}
	var raw bytes.Buffer
	stage, category, err := restoreTrialWith(context.Background(), restoreTestRequest(t), ops, &raw)
	if err != ErrRestore || stage != "pg_restore" || category != "missing database role" || !strings.Contains(raw.String(), "secret-user") {
		t.Fatalf("stage=%q category=%q err=%v diagnostic=%q", stage, category, err, raw.String())
	}
}

func TestSelectedAuthDataTrialRejectsUnsupportedTablesBeforeConnection(t *testing.T) {
	stage, category, rows, err := RestoreAuthDataTableTrial(context.Background(), RestoreRequest{}, "users;DROP SCHEMA auth", nil)
	if err != ErrRestore || stage != "input validation" || category != "" || rows != 0 {
		t.Fatalf("stage=%q category=%q rows=%d err=%v", stage, category, rows, err)
	}
}

func TestRestoreTrialContinueUsesNonAtomicMode(t *testing.T) {
	ops := recoveryTestOps()
	ops.observe = func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
		return emptyRecoveryObservation(), nil
	}
	ops.run = func(_ context.Context, request tools.RunRequest) (tools.RunResult, error) {
		if request.Mode != tools.ModeRestoreContinue {
			t.Fatalf("restore mode = %d", request.Mode)
		}
		_, _ = request.Stderr.Write([]byte("pg_restore: error: schema auth already exists\n"))
		return tools.RunResult{}, tools.ErrRun
	}
	stage, category, err := restoreTrialContinueWith(context.Background(), restoreTestRequest(t), ops, nil)
	if err != ErrRestore || stage != "pg_restore" || category != "existing object conflict" {
		t.Fatalf("stage=%q category=%q err=%v", stage, category, err)
	}
}

func TestRestoreTrialDoesNotRunWhenPreflightRejectsTarget(t *testing.T) {
	ops := recoveryTestOps()
	observation := emptyRecoveryObservation()
	observation.Relations = []RelationObservation{{Schema: "public", Name: "already_there"}}
	ops.observe = func(context.Context, ConnectionParams, []byte, []string) (CatalogObservation, error) {
		return observation, nil
	}
	ops.run = func(context.Context, tools.RunRequest) (tools.RunResult, error) {
		t.Fatal("must not run pg_restore")
		return tools.RunResult{}, nil
	}
	stage, category, err := restoreTrialWith(context.Background(), restoreTestRequest(t), ops, nil)
	if err != ErrRestore || stage != "target preflight" || category != "" {
		t.Fatalf("stage=%q category=%q err=%v", stage, category, err)
	}
}
