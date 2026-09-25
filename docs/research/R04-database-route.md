# R04 — Supabase-aware database route

**Status:** open research decision. This records a conservative qualification target,
not a hosted-support claim or a tested capture/restore recipe.

## Initial target

| Dimension | Initial qualification target | Refusal / open evidence |
| --- | --- | --- |
| PostgreSQL source, target, and native client | PostgreSQL **17**, matching major for all three | Any other major, a client older than source, or a target downgrade is refused until separately qualified. The Task 02 research input is PostgreSQL 17.11 source, not an approved client payload. |
| Native tools | `pg_dump` custom archive plus `pg_restore`; `psql` only for a reviewed, pinned recipe | R02 has not selected, packaged, or proven a redistributable client. No tool is downloaded at runtime. |
| Route | Direct database route or a qualified **session** pooler | Transaction poolers are blocked. R03 must prove TLS and source/target identity for both pgx and native clients. |
| TLS | Explicit hostname/trust verification for both Go and subprocess connections | No insecure fallback; pooler TLS does not prove pooler-to-backend identity or encryption. R03 is open. |
| Source/destination | Hosted Supabase source and new, otherwise unused hosted destination | No hosted route has been contacted for this record. R04/R05 must establish the recipe and baseline. |
| `public` | Ordinary provider-owned `public` application support is required before any hosted database claim | Historical non-public fixtures do not qualify this. R04 and R05 block it. |

PostgreSQL permits broader dump/client combinations in some circumstances, but SPARC
will not infer a broader policy. A release may ship only a separately qualified client
payload and exactly documented majors.

## Route decision and boundaries

The candidate route is a fixed, Supabase-aware native recipe: use native custom-format
capture/restore for compatible database state, and separate only the specifically
qualified managed/service artifacts. `pg_dump` does not include globals; roles, managed
baselines, migration history, Auth, Storage, Vault, and service configuration need their
own qualified handling. No generic SQL rewrite engine, unfiltered upstream script copy,
or permissive restore-error handling is authorized.

A fresh local two-cluster PostgreSQL 17 test (`TestCrossClusterRestoreRequiresTargetOwner`)
now demonstrates this failure directly. A native custom-format dump of a synthetic
schema owned by a `NOLOGIN` role fails on a separate empty target without that role;
`--exit-on-error --single-transaction` leaves the target schema absent. After
explicitly provisioning the synthetic owner role in the target, the same dump
restores its row and relation owner with the source cluster shut down. The
existing `sparc-localdemo` runs its source and target *databases in one cluster*,
so it could not reveal missing cluster-wide roles. This is a recipe blocker,
not a license to copy source roles or bypass ownership. No hosted Supabase
baseline or role-provisioning policy has been qualified.

The source is observed read-only. Capture needs a coherent snapshot or a documented quiet
window; separate passes do not imply cross-service atomicity. Restore uses target and
archive inputs only, refuses nonempty/ambiguous targets, and remains quarantined after a
failed or uncertain mutation. A command exit is not a security or recovery-equivalence
result.

## Evidence available versus required

Historical evidence in the preserved ledger established a narrow local PG17.9
non-public fixture and a smaller hosted PG17.6/17.9 observation. It did not establish
ordinary `public`, real Auth, Storage ownership, Vault, Functions, hosted baselines,
source-network denial, Windows, or general Supabase support. It is context only and is
not inherited success.

Before this route can be advertised, record fresh, authorized evidence for:

1. **R03**: direct/session route classification, strict TLS and project binding for pgx
   and native clients.
2. **R04**: a pinned recipe covering `public`, managed exclusions/customizations,
   roles, migration history, Auth-compatible data, extensions, and snapshot/quiet-window
   behavior.
3. **R05**: destination owners, defaults, membership, column grants, RLS and
   security-definer behavior, including a negative exposure check.
4. **R06**, **R07**, **R08**, and **R09**: real Auth, standard Storage ownership,
   Vault-if-detected, and dependency-complete Function packages respectively.
5. **R12** and **R15**: safe side-effect handling and specialty/extension durable-data
   detection/refusal.

Sources to recheck when running the experiments: PostgreSQL 17 `pg_dump` and
`pg_restore` manuals (P1/P2 in the research ledger), Supabase backup/restore guidance
(S1), and the pinned upstream route source identified in that ledger. This document makes
no claim that those sources alone prove compatibility.
