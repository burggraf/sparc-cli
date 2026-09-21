# Compatibility qualification target

SPARC has **no hosted Supabase support claim yet**. This is the initial target
against which experiments qualify a future release; it is not a promise that a
current hosted project, archive, tool, or route works.

## Initial database profile

| Item | Qualification target | Current result |
| --- | --- | --- |
| Source PostgreSQL | 17 major | Not qualified |
| Destination PostgreSQL | 17 major, matching source | Not qualified |
| Bundled native client | 17 major, matching source and destination | Not selected or qualified; R02 remains open |
| Database route | Direct route or qualified session pooler | Not qualified; R03 remains open |
| Transaction pooler | Never in this profile | Blocked |
| TLS/project identity | Explicitly verified by Go and native client | Not qualified; R03 remains open |
| Ordinary hosted `public` | Required before hosted database support | Not qualified; R04/R05 remain open |
| Destination | New, otherwise unused hosted project | Future target safety gate; R05/A22 apply |

A client older than source, destination downgrade, major mismatch, unknown route, or
unknown server capability is a refusal, not compatibility. PostgreSQL compatibility rules
are broader than this deliberately narrow policy. A session-pooler route enters the
profile only after its own evidence; it does not inherit direct-route qualification.

## Product gates

The following are product gates, not optional polish:

- **Real Auth:** recovered IDs, identities, password hashes and qualified behavior must
  pass R06. Historical or API-only user creation is insufficient.
- **Standard Storage with ownership:** bytes, settings, metadata, IDs and owner/RLS
  behavior must pass R07. Service-key upload with lost ownership is not application-ready
  recovery.
- **Functions:** recovery requires a dependency-complete deployable package and qualified
  deployment behavior under R09; a body without imports/assets is incomplete.
- **Vault when detected:** ciphertext without the required active-source recovery material
  is blocked by R08. The report must say `missing_critical_material`, never complete.

Specialty Storage vectors, Iceberg/analytics, external/foreign data, and unqualified
extension durable data are unsupported only when reliably detected and reported as an
explicit incomplete/refusal condition (R15). Their absence from a capture result is never
support.

## Evidence interpretation

Report evidence levels follow `docs/05-VERIFICATION-AND-SECURITY.md`: documented and
offline/local tests are not hosted or source-independent recovery proof. There is no
payload, clean-machine, scale, release, or inherited historical success claim here.
A future compatibility entry must name versions, route, client digest, command/exit
status, omissions, and the applicable A01–A30 evidence.
