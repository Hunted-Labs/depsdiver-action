# Scan Go Imports Action

A GitHub Action that scans all Go files in your project and extracts all import statements, generating a comprehensive report.

## Features

- 🔍 Recursively scans all `.go` files in your project   
- 📊 Generates a detailed markdown report with imports organized by file
- 📈 Provides summary statistics (total imports, unique imports)  
- 🎯 Automatically skips `vendor/`, `.git/`, and `node_modules/` directories
- 📦 Uploads the report as a downloadable artifact
- ✨ Displays a summary in the GitHub Actions UI
- 🔒 Integrates with HLTI API to fetch threat intelligence data for GitHub packages hello

## Usage
- ✨ Displays a summary in the GitHub Actions UI hello
- 🔒 Integrates with HLTI API to fetch threat intelligence data for GitHub packages 
  
## Usage 

### Basic Usage

```yaml
name: Scan Go Imports

on:
  push:
    branches: [ main ]
  pull_request:
    branches: [ main ]

jobs:
  scan-imports:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: Scan Go imports
        uses: your-username/hl-action@v1
```

### Advanced Usage with HLTI API

```yaml
name: Scan Go Imports

on:
  push:
    branches: [ main ]
  workflow_dispatch:

jobs:
  scan-imports:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: Scan Go imports
        id: scan
        uses: your-username/hl-action@v1
        with:
          path: './src'                    # Optional: directory to scan (default: '.')
          output-file: 'imports.txt'       # Optional: output file name (default: 'go-imports-report.txt')
          artifact-name: 'import-report'   # Optional: artifact name (default: 'go-imports-report')
          artifact-retention-days: '7'     # Optional: artifact retention days (default: '30')
          hlti-api-url: 'https://your-api-url.com'  # Required: HLTI API base URL
          hlti-token: ${{ secrets.HLTI_TOKEN }}     # Required: HLTI API token (recommended to use secret)
      
      - name: Use scan results
        run: |
          echo "Total imports: ${{ steps.scan.outputs.total-imports }}"
          echo "Unique imports: ${{ steps.scan.outputs.unique-imports }}"
          echo "Report file: ${{ steps.scan.outputs.report-file }}"
```

### Setting Up HLTI API Token

#### Option 1: Using GitHub Secrets (Recommended)

1. Go to your repository on GitHub
2. Navigate to **Settings** → **Secrets and variables** → **Actions**
3. Click **New repository secret**
4. Name: `HLTI_TOKEN`
5. Value: Your HLTI API token (should start with `hl_` and be 59 characters)
6. Click **Add secret**

Then in your workflow, the token will be automatically used if you don't specify `hlti-token` input:

```yaml
- name: Scan Go imports
  uses: your-username/hl-action@v1
  with:
    hlti-api-url: 'https://your-api-url.com'
    # Token is automatically pulled from secrets.HLTI_TOKEN
```

#### Option 2: Explicitly Pass Token

You can also explicitly pass the token as an input:

```yaml
- name: Scan Go imports
  uses: your-username/hl-action@v1
  with:
    hlti-api-url: 'https://your-api-url.com'
    hlti-token: ${{ secrets.HLTI_TOKEN }}
```

#### Option 3: Organization/Repository Secrets

For organization-wide or repository-level secrets:
- **Organization secrets**: Settings → Secrets and variables → Actions → New organization secret
- **Repository secrets**: Settings → Secrets and variables → Actions → New repository secret

The action will automatically use `secrets.HLTI_TOKEN` if available, even if not explicitly passed.

## Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `path` | Directory path to scan | No | `.` |
| `output-file` | Output file name for the import report | No | `go-imports-report.txt` |
| `artifact-name` | Name of the artifact to upload | No | `go-imports-report` |
| `artifact-retention-days` | Number of days to retain the artifact | No | `30` |
| `hlti-api-url` | HLTI API base URL (e.g., `https://api.example.com`) | Yes* | `https://api.example.com` |
| `hlti-token` | HLTI API token (should be set as secret) | Yes* | (uses `secrets.HLTI_TOKEN` if available) |

\* Required if you want to query threat intelligence data for GitHub packages. If not provided, the action will still scan imports but won't query the API.

## Outputs

| Output | Description |
|--------|-------------|
| `report-file` | Path to the generated report file |
| `total-imports` | Total number of import statements found |
| `unique-imports` | Number of unique imports found |

## Report Format

The generated report includes:

1. **Header**: Generation timestamp
2. **File-by-file listing**: All imports organized by source file (excluding standard library and GitHub packages)
3. **Summary**: Total and unique import counts, plus GitHub packages and standard library counts
4. **GitHub Packages section**: List of all GitHub packages with threat intelligence data (if API is configured)
5. **Third-party imports list**: Alphabetically sorted list of non-stdlib, non-GitHub imports

Example report:

```markdown
# Go Imports Report
(Standard library and GitHub packages filtered out)
Generated: 2024-01-15 10:30:45 UTC

## File: utils/helper.go

- `golang.org/x/crypto`

---

## Summary

Total third-party imports (excluding stdlib and GitHub): 1
Unique third-party imports: 1
GitHub packages found: 3
Standard library packages found: 5

### GitHub Packages

- `github.com/example/easygo/pkg/helpers`
  - Repository ID: 12345678
  - Repository: https://github.com/example/easygo
  - Geocoded Locations: 2 found
    - United States (US)
    - Canada (CA)

- `github.com/mailru/easyjson`
  - Repository ID: 87654321
  - Repository: https://github.com/mailru/easyjson
  - Geocoded Locations: 1 found
    - Russia (RU)

### All Unique Third-Party Imports (excluding stdlib and GitHub)

- `golang.org/x/crypto`
```

## Publishing

To publish this action:

1. Create a new repository on GitHub
2. Push this code to the repository
3. Create a release tag (e.g., `v1`, `v1.0.0`)
4. Use the action in other repositories with:
   ```yaml
   uses: your-username/hl-action@v1
   ```

For versioning, it's recommended to use:
- `@v1` - points to the latest v1.x.x release
- `@v1.0.0` - points to a specific version
- `@main` - points to the latest commit on main branch (not recommended for production)

## License

MIT License - see [LICENSE](LICENSE) file for details

