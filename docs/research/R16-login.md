# R16 — Management API authentication (token-only foundation)

**Status:** user-supplied token is the initial implementation candidate; no live
Supabase authentication was performed.

The existing research ledger records that the Management API integration guide
requires a client ID and confidential client secret for its OAuth exchange. SPARC
will not embed such a secret in a public executable. Task 08 therefore supports
only an explicitly supplied Management API token accepted by `supabase.NewClient`;
the future CLI caller is expected to resolve it through `credentials.Input`. No
OAuth client, browser callback, device flow, token
refresh, or broker is implemented.

The token is bounded printable ASCII, held in process memory, and sent only in
the `Authorization: Bearer` header to the fixed HTTPS Management API endpoint.
The client disables ambient proxy configuration, rejects redirects, applies a
request timeout, and never includes the token or response body in public errors.
The token is not persisted by this package. No `SUPABASE_ACCESS_TOKEN` or other
ambient environment lookup is performed here; a caller must choose one explicit
credential source.

Management API tokens, database passwords, project service/S3 keys, and backup
destination credentials are distinct. This client accepts no credentials for
the latter three roles. Authentication scopes and precise least-privilege token
requirements remain unverified until current documentation and an authorized
read-only service test are reviewed. A 401 authentication failure, 403 permission
denial, and an empty successful project list remain distinct outcomes.

Any live test requires separate owner approval for the specific token, account,
project visibility, permitted GET requests, and test timing. No token, account,
network call, or hosted service was used to implement or test this candidate.
