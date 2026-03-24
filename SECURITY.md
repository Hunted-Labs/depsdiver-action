# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in this GitHub Action, please report it responsibly by emailing **support@depsdiver.com** rather than opening a public issue.

Please include:
- A description of the vulnerability
- Steps to reproduce
- Potential impact

We will acknowledge your report within 48 hours and aim to release a fix within 14 days for confirmed vulnerabilities.

## Supported Versions

| Version | Supported |
|---------|-----------|
| `@v1` (latest) | ✅ |
| Older tags | ❌ |

## Security Considerations for Users

- Always pin to a specific version tag (e.g., `@v1`) rather than `@main` in production workflows
- Store your DepsDiver API token as a GitHub Actions secret, never hardcoded in the workflow file
- The action only requires read access to your repository. Do not grant it write permissions
