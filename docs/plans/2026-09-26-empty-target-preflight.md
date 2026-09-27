# Empty Target Preflight Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Add a local-only, read-only preflight that accepts an empty designated
application scope without hardcoding a Supabase baseline.

**Architecture:** Add `EmptyTargetScopeV1` and `CheckEmptyTargetV1` alongside the
existing narrow `CheckTargetSecurityV1`. The new evaluator validates a declared
scope against an existing `CatalogObservation`: required schemas such as `public`
must be present and empty, requested custom schemas must be absent, and the
existing `PUBLIC SELECT` refusal still applies. It cannot mutate a target and is
not wired to the CLI or restore path.

**Tech Stack:** Go standard library, existing `internal/database` read-only pgx
catalog observer, disposable PostgreSQL 17 integration fixture.

---

### Task 1: Define the fail-closed scope contract

**Files:**
- Modify: `internal/database/security.go`
- Test: `internal/database/security_test.go`

**Step 1: Write failing table tests for a valid empty scope.**

Add a `CatalogObservation` fixture with PostgreSQL 17, verified TLS, a read-only
transaction, observed security facts, an empty present `public` schema, and an
absent `app` schema. Assert:

```go
scope := EmptyTargetScopeV1{
    RequiredPresent: []string{"public"},
    RequiredAbsent:  []string{"app"},
}
if err := CheckEmptyTargetV1(observation, scope); err != nil {
    t.Fatalf("empty scope = %v", err)
}
```

**Step 2: Run the focused test to verify RED.**

Run:

```sh
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
go test -mod=readonly ./internal/database \
  -run '^TestCheckEmptyTargetV1' -count=1 -v
```

Expected: build failure because `EmptyTargetScopeV1` and
`CheckEmptyTargetV1` do not exist.

**Step 3: Add the minimum contract and validation.**

Define:

```go
type EmptyTargetScopeV1 struct {
    RequiredPresent []string
    RequiredAbsent  []string
}
```

Require at least one required-present schema. Reject invalid UTF-8/control
characters, duplicates, and names appearing in both lists with
`ErrTargetSecurityUnknown`. Reuse the existing strict identifier validation;
do not add a second parser or accept patterns.

**Step 4: Re-run the focused test to verify GREEN.**

Run the focused command from Step 2. Expected: PASS.

**Step 5: Commit.**

```sh
git add internal/database/security.go internal/database/security_test.go
git commit -m "feat: define empty target scope preflight"
```

### Task 2: Refuse observed application state and unsafe grants

**Files:**
- Modify: `internal/database/security.go`
- Test: `internal/database/security_test.go`

**Step 1: Write failing cases.**

Use the same valid fixture, changing one fact per subtest. Each must return
`ErrUnsafeTargetSecurityProfile`:

- `public` is missing;
- requested `app` schema is present;
- a relation is observed in `public`;
- a routine is observed in `public`;
- `Security.PublicSelectDefaults`, `PublicSelectObjects`, or
  `PublicSelectColumns` is true.

Add malformed-observation cases (duplicate/missing schema observations,
unsupported server/TLS/read-only/security state, relation/routine with an
invalid identity) expecting `ErrTargetSecurityUnknown`.

**Step 2: Run the focused test to verify RED.**

Run the command from Task 1, Step 2. Expected: the new unsafe cases fail because
only the narrow `PUBLIC SELECT` check exists.

**Step 3: Implement the comparator.**

`CheckEmptyTargetV1` must call `CheckTargetSecurityV1` first. Build an internal
map only after validating that every declared scope name occurs exactly once in
the observation. Reject a missing required-present schema, a present
required-absent schema, any relation in a required-present schema, or any
routine in a required-present schema. Ignore objects outside the declared
application scope; they may be provider-managed baseline state. Do not compare
owners, named roles, memberships, RLS predicates, ACLs other than the existing
`PUBLIC SELECT` check, Auth/Storage/Functions resources, or policy/routine
bodies.

**Step 4: Re-run the focused test to verify GREEN.**

Run the command from Task 1, Step 2. Expected: PASS.

**Step 5: Commit.**

```sh
git add internal/database/security.go internal/database/security_test.go
git commit -m "feat: reject nonempty target application scope"
```

### Task 3: Prove the gate against the local PostgreSQL observer

**Files:**
- Modify: `internal/database/inspect_integration_test.go`
- Modify: `docs/research/R05-permissions.md`
- Modify: `docs/progress.md`

**Step 1: Add the failing local integration case.**

Create a disposable TLS PostgreSQL fixture. First observe `public` plus a
nonexistent `sparc_app` schema and assert `CheckEmptyTargetV1` accepts it. Seed
one synthetic table in `public`, re-observe, and assert
`ErrUnsafeTargetSecurityProfile`. Query the fixture before and after each
observation to prove the observer did not change the seeded marker; no restore
is invoked.

**Step 2: Run the focused integration test to verify RED.**

Run:

```sh
SPARC_TEST_PG_BIN=/opt/homebrew/opt/postgresql@17/bin \
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
go test -mod=readonly -tags=integration ./internal/database \
  -run '^TestEmptyTargetPreflightOnDisposablePostgres$' -count=1 -v
```

Expected: failure before the comparator recognizes relations in a required
present schema.

**Step 3: Implement only the test fixture additions required by the gate.**

Use the existing `newPostgresFixture`, explicit TLS configuration, and `pgx`
administrator connection. Do not introduce a restore helper, a generic policy
engine, hosted configuration, or target mutation by product code.

**Step 4: Re-run focused unit and integration tests to verify GREEN.**

Run the Task 1 focused unit command and the Task 3 focused integration command.
Expected: PASS.

**Step 5: Record evidence and limits.**

Update R05/progress to state that the new preflight checks only declared
application-scope emptiness and narrow `PUBLIC SELECT` hazards. State that it
leaves the newly-created Supabase managed baseline unmodeled, is not restore
authorization, and needs separately authorized hosted qualification.

**Step 6: Commit.**

```sh
git add internal/database/inspect_integration_test.go \
  docs/research/R05-permissions.md docs/progress.md
git commit -m "test: prove empty target preflight locally"
```

### Task 4: Fresh guarded verification and final evidence

**Files:**
- Modify only if verification exposes a documented correction.

**Step 1: Format and check the diff.**

```sh
gofmt -w internal/database/security.go internal/database/security_test.go \
  internal/database/inspect_integration_test.go
git diff --check
```

Expected: no output.

**Step 2: Run fresh suites.**

```sh
export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off
export SPARC_TEST_PG_BIN=/opt/homebrew/opt/postgresql@17/bin
go test -mod=readonly ./... -count=1
go test -mod=readonly -tags=integration ./internal/database -count=1
go test -race -mod=readonly -tags=integration ./internal/database -count=1
go vet ./...
go vet -tags=integration ./internal/database
```

Expected: all commands exit 0 on the current macOS arm64 developer host.

**Step 3: Review the committed change.**

```sh
git status --short --branch
git show --check --stat HEAD
```

Confirm no credential, client payload, archive, hosted configuration, or restore
implementation is present.

**Step 4: Push under existing authorization.**

```sh
git push origin main
```
