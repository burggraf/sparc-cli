# Empty Target Preflight Design

**Status:** approved local-only design (2026-09-26).

## Goal

Refuse a restore target unless its designated application scope is empty, while
leaving the provider-managed Supabase baseline untouched and unclaimed.

## Boundary

SPARC will not encode a universal Supabase owner/ACL/role/profile baseline.
Those facts are provider- and project-version-specific and must be qualified on
an explicitly authorized newly created hosted project before they can support a
hosted restore claim.

The local candidate instead evaluates a caller-supplied application scope:

- required existing schemas, initially only `public`, must be present and have
  no relations or routines;
- requested custom application schemas must be absent;
- the existing narrow `PUBLIC SELECT` default/relation/column refusal remains
  in force for the observed selected scope.

The check is read-only and returns a fixed unsafe/unknown error. It never
creates, drops, revokes, changes ownership, or normalizes target state. A
passing result is only an empty-application-scope prerequisite; it is not
Supabase baseline validation, restore authorization, or hosted qualification.

## Data flow

A small `EmptyTargetScopeV1` value names `RequiredPresent` and
`RequiredAbsent` schemas. It is validated as strict, unique literal PostgreSQL
identifiers with no overlap. `CheckEmptyTargetV1` accepts this scope and an
already collected `CatalogObservation` from the existing bounded, TLS-verified,
read-only observer.

The evaluator first requires PostgreSQL 17, TLS, a read-only observation,
nonempty schema facts, and the existing observed `PUBLIC SELECT` security
facts. Missing/contradictory observation or invalid scope returns
`ErrTargetSecurityUnknown`. It then rejects missing required-present schemas,
present required-absent schemas, any relation/routine in a required-present
schema, or the narrow existing `PUBLIC SELECT` findings with
`ErrUnsafeTargetSecurityProfile`.

It intentionally does not compare database/schema/object owners, named roles,
memberships, RLS predicates, service roles, Auth/Storage/Functions resources,
or provider-managed objects. Those require a concrete hosted baseline and stay
outside this gate.

## Tests and evidence

Offline tests cover malformed/overlapping scopes, incomplete observations,
empty `public` plus absent custom schema acceptance, and each unsafe state.
A local PostgreSQL 17 TLS fixture seeds a `public` relation and verifies that
its read-only observation is rejected; the existing direct/global/column
`PUBLIC SELECT` counterexamples remain refusal evidence. Tests do not execute
a restore or mutate through the evaluator.

No hosted connection, client payload, archive, or production CLI behavior is
part of this design.
