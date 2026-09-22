# R10 — Supabase Management API contract (offline candidate)

**Status:** transport and fixture contract implemented; live endpoint contract
not revalidated. No Supabase request or credential was used for Task 08.

The earlier research ledger identifies the official Management API v1 OpenAPI
source at <https://api.supabase.com/api/v1-json> and Management API
introduction. Those sources were not fetched or checked again here. All exact
routes, query parameters, and response fields below are **candidate fixture
assumptions**, not claims about the current service. Recheck the official
OpenAPI and obtain separate owner authorization before any live request.

## Candidate read-only request

- Fixed base URL: `https://api.supabase.com/v1`.
- `GET /projects?limit=100&offset=N`, starting at `offset=0` and continuing
  while a page contains 100 records.
- Candidate required project fields: `id`, `name`, `organization_id`,
  `region`, and `status`; optional `db_version` maps to a database-version
  observation. A missing version is **unknown**, not “unsupported.” The feature
  inventory state remains unknown because no feature endpoint/schema has been
  verified; unrecognized field names are not interpreted as features.
- Unknown project fields are returned as field names only. Their values and the
  original response body are not retained or logged.
- The list is bounded to 100 records/request, 10,000 projects, 2 MiB per
  response and 16 MiB total response data. A full final page at the cap is
  refused rather than silently treated as complete.

## Transport and errors

The client accepts one explicit Management API token. It uses standard HTTPS
certificate and hostname verification, a 30-second request timeout, and a
fixed endpoint. It ignores ambient HTTP proxy settings, follows no redirects,
and provides no user-configurable endpoint. Tests may inject `httptest` servers
inside the package only. It sends no DB, project service/S3, or backup-destination
credentials.

401, 403, 404, 429 and server failures have separate fixed error categories.
A 403 is not converted to an empty project list. A successful empty list means
only that the token's list response contained no visible projects; it does not
prove project nonexistence. `Retry-After` is parsed and capped at one hour, but
requests are not automatically retried. Response bodies and token bytes are
excluded from public errors.

The client keeps returned metadata in memory only. It does not archive or persist
raw API responses. Project names/refs and returned unknown-field names remain
sensitive operational metadata and must not be written to logs or issue reports.

## Qualification gates

No live service behavior, token scope, pagination stability, project-version
field, feature discovery, or permission matrix is qualified. The candidate list
response does not enumerate every project capability; unknown fields remain
unknown. No OAuth/browser flow, mutation endpoint, service configuration
capture, or hosted project test is included. A live contract test requires fresh
owner approval for the exact account/project, scope, and permitted read-only
request; it is not implied by these local `httptest` fixtures.
