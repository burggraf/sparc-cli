# Trusted tools and bounded execution

Task 05 provides synthetic-only payload-cache and process-runner mechanisms. It does not enable a CLI operation or establish PostgreSQL/Supabase client support. The compiled production payload inventory is empty.

## Runner boundary

`tools.Run` accepts a typed `PGDump`, `PGRestore`, or `PSQL` selection, but currently permits only the version invocation (`--version`). Callers cannot supply an executable path, arguments, environment, working directory, stdin, or shell command. Connected PostgreSQL invocations and scoped passfiles remain Task 6 work.

A package is addressed as `<NativeLocations.CacheDir>/tools-v1/<package-id>/`. Extraction uses a private random sibling staging directory, bounded streaming, exact manifest lengths and SHA-256 digests, executable-format/CPU checks, a receipt written last, and atomic no-replace publication. Every cache reuse and every launch revalidates the complete package and selected executable; an invalid published package is neither repaired nor replaced. The parser ceilings are 1,024 files, 240-byte portable paths, 256 MiB per file, 512 MiB compressed, and 1 GiB expanded. These are limits, not payload-size or support claims.

Each run uses `<NativeLocations.CacheDir>/operations-v1/.operation-<random>/` with private `home`, `tmp`, and `config` subdirectories. The operation directory is removed after process and stream cleanup. The process receives a direct trusted executable path, that operation directory as its working directory, EOF stdin, private HOME/temp/config values, and fixed `LANG=C`/`LC_ALL=C`. It inherits no ambient PATH, PostgreSQL variables, credential variables, loader variables, proxy variables, HOME, or Windows `SystemRoot`; no ambient executable or credential fallback is available.

Public failures are fixed, non-wrapping diagnostics: `tool payload unavailable`, `invalid tool payload`, `tool extraction failed`, `tool run failed`, `tool output failed`, and the platform lifecycle error `process unavailable`. Native errors, paths, arguments, stderr, and sink errors are not returned.

## Output and lifecycle policy

Stdout and stderr have separate positive byte limits and separate fixed 32 KiB pump buffers. Each pump reads at most `remaining+1`; stdout forwards exactly the allowed prefix before reporting overflow, while stderr is counted and discarded. A successful `RunResult` is produced only after both pumps and the process waiter join, and contains the exact final exit code and byte counts. **Any Run error returns a zero `RunResult`.** Stdout is synchronous and cannot be rolled back: before a later failure, its sink may already have received only the bounded permitted prefix. No failed result claims counts for either stream.

`OutputSink.WriteContext` and `OutputSink.CloseContext` must stop when their context is canceled. This requirement is what makes blocked output and sink close bounded; the runner does not create an unkillable wrapper goroutine. The operation timeout covers validation, setup, execution, and pumping. `CleanupTimeout` is positive and capped at five seconds; it bounds normal cleanup and sink close. If native process ownership is still unresolved when that cleanup deadline expires, the exceptional fail-stop path retries hard process-tree closure and joins owned wait/pump workers before returning. That security path may exceed `CleanupTimeout` rather than release a live owned process tree.

## Security and qualification limits

The final revalidation-to-execution interval is not confinement against a malicious process running as the same user. The private-storage boundary also does not protect against root/SYSTEM/administrator access. Task 04's macOS extended/inherited ACL qualification remains open, so POSIX modes alone are not a release claim for real credentials.

On Darwin, native tests cover direct launch, process-group termination, direct-child reaping, and same-group descendants retaining pipes. Deliberately detached descendants and abnormal parent death remain outside that mechanism and unqualified. On Windows, Job Object, handle-inheritance, argv/environment, reparse/DACL, and descendant behavior currently have compile/fault-fixture evidence only; native Windows runtime qualification remains required.

All payloads executed by these tests are synthetic Go test helpers. This evidence does not qualify a real PostgreSQL payload's provenance, redistribution, signatures/notarization, dependency loading, TLS, noninteractive behavior, or Supabase compatibility. Task 6 passfiles/structured connected invocation and those native/real-payload qualification items remain unimplemented.
