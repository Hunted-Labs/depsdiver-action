# DepsDiver Dependency FOCI Scanner

A GitHub Action that scans package manager files in your repository and queries the [DepsDiver](https://huntedlabs.com) API to detect Foreign Ownership, Control, or Influence (FOCI) in your dependencies.

## Features

- Scans package manager files across all major ecosystems
- Prefers lock files over manifests. When a lock file is present, the corresponding manifest is skipped to avoid duplicates and ensure transitive dependencies are included
- Reports FOCI presence and per-country contribution analysis per package
- Links directly to the full DepsDiver report for each flagged dependency
- Generates a markdown report and GitHub Actions step summary
- Uploads the report as a downloadable artifact
- Caches API results automatically between runs. Only newly added or changed packages are queried, keeping repeat scans fast
- Automatically skips `vendor/`, `.git/`, `node_modules/`, `target/`, `build/`, `dist/`, `.idea/`, and `__pycache__/` directories

## Supported Ecosystems

| Ecosystem | Manifest (used if no lock file) | Lock File (preferred, includes transitive deps) |
|-----------|----------------------------------|--------------------------------------------------|
| Go | `go.mod` | — |
| npm | `package.json` | `package-lock.json`, `npm-shrinkwrap.json`, `yarn.lock` |
| PyPI | `requirements.txt`, `pyproject.toml`, `Pipfile` | `requirements.lock`, `requirements-lock.txt`, `Pipfile.lock`, `poetry.lock` |
| Cargo (Rust) | `Cargo.toml` | `Cargo.lock` |
| RubyGems | `Gemfile` | `Gemfile.lock` |
| Maven | `pom.xml` | — |
| NuGet (.NET) | `*.csproj`, `*.vbproj`, `*.fsproj` | — |
| Gradle | `build.gradle`, `build.gradle.kts`, `libs.versions.toml` | — |

## Usage

### Basic Usage

```yaml
name: Scan Dependencies for FOCI

on:
  push:
    branches: [ main ]
  pull_request:
    branches: [ main ]

jobs:
  foci-scan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6

      - name: Scan dependencies
        uses: Hunted-Labs/depsdiver-action@v1
        with:
          depsdiver-api-url: 'https://depsdiver.com/api'
          depsdiver-token: ${{ secrets.DEPSDIVER_TOKEN }}
```

### Advanced Usage

```yaml
name: Scan Dependencies for FOCI

on:
  push:
    branches: [ main ]
  workflow_dispatch:

jobs:
  foci-scan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6

      - name: Scan dependencies
        id: scan
        uses: Hunted-Labs/depsdiver-action@v1
        with:
          path: '.'                              # Directory to scan (default: '.')
          output-file: 'foci-report.txt'         # Report file name (default: 'deps-foci-report.txt')
          artifact-name: 'foci-report'           # Artifact name (default: 'deps-foci-report')
          artifact-retention-days: '7'           # Artifact retention (default: '30')
          depsdiver-api-url: 'https://depsdiver.com/api'
          depsdiver-token: ${{ secrets.DEPSDIVER_TOKEN }}
          foci-threshold: '10'                   # Only flag packages with >10% FOCI change ratio

      - name: Fail if FOCI detected
        if: steps.scan.outputs.foci-packages > 0
        run: |
          echo "FOCI detected in ${{ steps.scan.outputs.foci-packages }} package(s)"
          exit 1
```

### Setting Up the DepsDiver Token

1. Go to your repository on GitHub
2. Navigate to **Settings** → **Secrets and variables** → **Actions**
3. Click **New repository secret**
4. Name: `DEPSDIVER_TOKEN`
5. Value: Your DepsDiver API token
6. Click **Add secret**

For organization-wide access, use an organization secret instead.

## Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `path` | Directory path to scan | No | `.` |
| `output-file` | Output file name for the report | No | `deps-foci-report.txt` |
| `artifact-name` | Name of the uploaded artifact | No | `deps-foci-report` |
| `artifact-retention-days` | Days to retain the artifact | No | `30` |
| `depsdiver-api-url` | DepsDiver API base URL | No* | — |
| `depsdiver-token` | DepsDiver API token | No* | (uses `secrets.DEPSDIVER_TOKEN`) |
| `foci-threshold` | FOCI change ratio threshold (0–100%). Only packages exceeding this are flagged. Leave empty to flag all packages with any FOCI data. | No | — |

\* Without `depsdiver-api-url` and `depsdiver-token` the action will discover and list dependencies but won't query for FOCI data.

## Outputs

| Output | Description |
|--------|-------------|
| `report-file` | Path to the generated report file |
| `foci-packages` | Number of packages with FOCI detected |
| `total-packages` | Total number of dependencies found across all package manager files |

## Finding Your Results

After a workflow run completes:

1. Click the workflow run in the **Actions** tab of your repository
2. Select the **Summary** tab at the top of the run page — the FOCI analysis appears here inline with the following expandable sections:
   - **📂 Files Scanned** — every package manager file found and how many packages came from each
   - **📦 All Packages Scanned** — every package checked, grouped by ecosystem, with a status icon next to each: `✅` passed, `⚠️` FOCI detected, `—` not in DepsDiver database, `❌` API error
   - **🌍 FOCI Analysis Results** — detailed breakdown for each flagged package with countries of concern, contribution percentages, and a link to the full report on Hunted Labs
3. Scroll to **Artifacts** at the bottom of the Summary page to download the full report file

## Status Badge

Add a FOCI scan badge to your repository's README to show the current passing/failing state:

```markdown
[![FOCI Scan](https://github.com/{owner}/{repo}/actions/workflows/{workflow-file}.yml/badge.svg)](https://github.com/{owner}/{repo}/actions/workflows/{workflow-file}.yml)
```

Replace `{owner}`, `{repo}`, and `{workflow-file}` with your values. For example, if your workflow file is `.github/workflows/foci-scan.yml`:

```markdown
[![FOCI Scan](https://github.com/my-org/my-repo/actions/workflows/foci-scan.yml/badge.svg)](https://github.com/my-org/my-repo/actions/workflows/foci-scan.yml)
```

The badge reflects the workflow's pass/fail status. To make it fail when FOCI is detected, add the enforcement step from the [Advanced Usage](#advanced-usage) example.

## Report Format

The generated report includes:

1. **Summary** — total dependency count and FOCI statistics
2. **Detailed FOCI Analysis** — per-package breakdown for any package with FOCI detected, including:
   - Link to the full DepsDiver report on Hunted Labs
   - Total foreign contribution percentage
   - Countries of concern with per-country contribution breakdown
   - Repository FOCI locations (country, organization)
3. **Package Manager Dependencies** — full list of all discovered packages grouped by ecosystem, annotated with FOCI status if queried

Example summary section:

```markdown
# Dependency FOCI Report
Generated: 2026-03-12 10:30:45 UTC

## Summary

### Files Scanned

- `go.mod` (12 packages)
- `package.json` (23 packages)
- `requirements.txt` (7 packages)

Package manager dependencies found: 42

### FOCI Analysis

Passed: 32
FOCI detected: 2
Not in DepsDiver database: 8
Total repository FOCI locations: 3
```

## Versioning

Use `@v1` for the latest v1.x release, `@v1.0.0` for a pinned version, or `@main` for the latest (not recommended for production).

## License

MIT License — see [LICENSE](LICENSE) for details.
