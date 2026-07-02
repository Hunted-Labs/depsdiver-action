# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in this GitHub Action, please report it responsibly by emailing **support@depsdiver.com** rather than opening a public issue.

Please include:
- A description of the vulnerability
- Steps to reproduce
- Potential impact

## Supported Versions

Only the latest major release line receives fixes. Older versions are not maintained — please stay on the latest.

| Version | Supported |
|---------|-----------|
| `@v3` (latest) | ✅ |
| `@v2` and older | ❌ |

`@v3` is a breaking change from `@v2`: scanning is performed by the [`diver`](https://huntedlabs.com/diver-cli) CLI (downloaded at runtime) and `depsdiver-token` is now **required**. Upgrading from v2 without providing a token will fail.

## Security Considerations for Users

- Always pin to a specific version, ideally a commit SHA (e.g., `@<sha> # v3.0.0`), or at minimum a version tag like `@v3` rather than `@main` in production workflows
- Store your DepsDiver API token as a GitHub Actions secret, never hardcoded in the workflow file
- The action only requires read access to your repository. Do not grant it write permissions
