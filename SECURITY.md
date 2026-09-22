# Security Policy

## Current scope

SPARC is not yet a backup or recovery tool. The current command shell has no operational backup, verify, or restore path. Offline synthetic tests cover limited private-storage, trusted-payload, process-lifecycle, runner, and scoped-passfile mechanisms only; they do not qualify real credentials, PostgreSQL client payloads, hosted Supabase behavior, or recovery. See [credential boundaries](docs/credentials.md), [trusted tools](docs/tools.md), and [compatibility gates](docs/compatibility.md) for the current limits.

## Reporting a vulnerability

Do not include credentials, API tokens, recovery keys, passfiles, `.env` contents, project identifiers, archives, raw diagnostics, or customer data in a public issue. Use the repository host's private security advisory feature when available, or contact the repository owner through a private channel.

Please include a minimal sanitized reproduction, affected revision, impact, and any relevant command output with secrets removed. Do not attach real backup artifacts or project exports.

## Development hygiene

`.gitignore` is only a secondary control. Review every change before publishing and keep all credentials, private configuration, archives, and diagnostic data outside the repository.
