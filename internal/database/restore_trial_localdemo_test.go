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
