# Security Policy

## Supported versions

Security fixes are made for the latest released version. Users should upgrade
to the latest release before requesting a fix for an older version.

## Reporting a vulnerability

Use GitHub private vulnerability reporting as the primary channel:
<https://github.com/apimgr/api/security/advisories/new> (Repository → Security
→ Report a vulnerability). Do not file vulnerabilities as public issues or
pull requests. The application's `/.well-known/security.txt` also identifies
the configured security contact for operators of a running instance.

Maintainers will review the report privately, work with the reporter to
understand and reproduce the issue, coordinate a fix and release, and agree on
public disclosure after users have had an opportunity to update. Public
disclosure should avoid exposing exploit details before a fix is available.

For a running instance, use its `/server/security` page for the policy and
reporting workflow. The machine-readable contact details are at
`/.well-known/security.txt`; the application's security contact form is
`/server/contact?security_id=...` with the identifier supplied by the security
page.

Mirrors without private vulnerability reporting should direct reporters to the
GitHub origin repository above.
