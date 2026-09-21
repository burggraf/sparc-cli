# Security Policy

## Current scope

SPARC is scaffold-only and is not yet a backup or recovery tool. No security claim about backup, archive, credential, or recovery behavior is made by the current command shell.

## Reporting a vulnerability

Do not include credentials, API tokens, recovery keys, passfiles, `.env` contents, project identifiers, archives, raw diagnostics, or customer data in a public issue. Use the repository host's private security advisory feature when available, or contact the repository owner through a private channel.

Please include a minimal sanitized reproduction, affected revision, impact, and any relevant command output with secrets removed. Do not attach real backup artifacts or project exports.

## Development hygiene

`.gitignore` is only a secondary control. Review every change before publishing and keep all credentials, private configuration, archives, and diagnostic data outside the repository.
