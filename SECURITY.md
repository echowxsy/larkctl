# Security Policy

## Supported Versions

Only the latest release receives security fixes. Update with `larkctl upgrade` or grab the newest [GitHub Release](https://github.com/echowxsy/larkctl/releases).

## Reporting a Vulnerability

Please **do not** open a public issue for security problems. Use GitHub's private vulnerability reporting instead: **Security tab → Report a vulnerability** on this repository.

Include what you can: affected command/version, reproduction steps, and impact (e.g. token exposure, scope escalation). You should hear back within a week.

Session tokens and OAuth credentials live in `~/.lark/config.json` — treat that file as a secret when writing reproduction reports.
