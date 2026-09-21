# Packaging research only

No release package, release script, embedded client, or production extraction
code exists here. Task 02 has not selected a distribution format.

The isolated `experiments/packaging` smoke compares an adjacent synthetic
helper with a Go-embedded synthetic helper on macOS arm64. It is mechanical
evidence only: private new-directory extraction, SHA-256 checking, absolute-path
execution with a sanitized environment, and ad-hoc signature byte preservation.
It does not prove PostgreSQL dependency closure, Developer ID, notarization,
Gatekeeper/public trust, Windows trust, or a release-ready one-file package.

Adjacent `tools/` remains a control/fallback. It requires owner approval before
it can become a release format and does not by itself solve OS trust.

Trust observations must stay separate from extraction's dependency-offline
claim:

1. **Normal online validation:** fresh quarantined outer/helper evaluation with
   normal OS validation connectivity.
2. **First-ever offline validation:** fresh assessment state with dependency
   and OS-validation network unavailable; never substitute a prior assessment.
3. **Offline after cached assessment:** repeat after an online assessment,
   reported separately from first-ever offline behavior.

The current smoke makes no network request for dependencies, but it is not a
clean-machine or quarantined-artifact trust test.
