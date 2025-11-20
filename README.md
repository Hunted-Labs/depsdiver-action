# Scan Go Imports Action

A GitHub Action that scans all Go files in your project and extracts all import statements, generating a comprehensive report.

## Features

- 🔍 Recursively scans all `.go` files in your project
- 📊 Generates a detailed markdown report with imports organized by file
- 📈 Provides summary statistics (total imports, unique imports)
- 🎯 Automatically skips `vendor/`, `.git/`, and `node_modules/` directories
- 📦 Uploads the report as a downloadable artifact
- ✨ Displays a summary in the GitHub Actions UI

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

### Advanced Usage

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
      
      - name: Use scan results
        run: |
          echo "Total imports: ${{ steps.scan.outputs.total-imports }}"
          echo "Unique imports: ${{ steps.scan.outputs.unique-imports }}"
          echo "Report file: ${{ steps.scan.outputs.report-file }}"
```

## Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `path` | Directory path to scan | No | `.` |
| `output-file` | Output file name for the import report | No | `go-imports-report.txt` |
| `artifact-name` | Name of the artifact to upload | No | `go-imports-report` |
| `artifact-retention-days` | Number of days to retain the artifact | No | `30` |

## Outputs

| Output | Description |
|--------|-------------|
| `report-file` | Path to the generated report file |
| `total-imports` | Total number of import statements found |
| `unique-imports` | Number of unique imports found |

## Report Format

The generated report includes:

1. **Header**: Generation timestamp
2. **File-by-file listing**: All imports organized by source file
3. **Summary**: Total and unique import counts
4. **Unique imports list**: Alphabetically sorted list of all unique imports

Example report:

```markdown
# Go Imports Report
Generated: 2024-01-15 10:30:45 UTC

## File: main.go

- `fmt`
- `os`
- `strings`

## File: utils/helper.go

- `encoding/json`
- `net/http`

---

## Summary

Total import statements found: 5
Unique imports: 5

### All Unique Imports

- `encoding/json`
- `fmt`
- `net/http`
- `os`
- `strings`
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

