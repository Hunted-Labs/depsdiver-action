package main

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <directory>\n", os.Args[0])
		os.Exit(1)
	}

	rootDir := os.Args[1]
	allImports := make(map[string]map[string]bool) // file -> imports
	uniqueImports := make(map[string]bool)
	githubImports := make(map[string]bool)
	standardLibImports := make(map[string]bool)
	
	// Get HLTI API configuration from environment
	hltiToken := os.Getenv("HLTI_TOKEN")
	hltiAPIURL := os.Getenv("HLTI_API_URL")
	if hltiAPIURL == "" {
		hltiAPIURL = "https://api.example.com" // default, should be overridden
	}
	
	// Map to store API results for each GitHub import
	githubImportResults := make(map[string]*PackageInfo)

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip vendor, .git, and non-Go files
		if info.IsDir() {
			if info.Name() == "vendor" || info.Name() == ".git" || info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Parse the Go file
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			// Skip files that can't be parsed (might be in build tags, etc.)
			return nil
		}

		imports := make(map[string]bool)

		// Extract imports
		for _, imp := range node.Imports {
			importPath := strings.Trim(imp.Path.Value, "\"")
			
			// Categorize imports
			if isStandardLibrary(importPath) {
				standardLibImports[importPath] = true
			} else if isGitHubPackage(importPath) {
				githubImports[importPath] = true
			} else {
				// Only track non-standard, non-GitHub imports
				imports[importPath] = true
				uniqueImports[importPath] = true
			}
		}

		if len(imports) > 0 {
			// Make path relative to root
			relPath, _ := filepath.Rel(rootDir, path)
			allImports[relPath] = imports
		}

		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error walking directory: %v\n", err)
		os.Exit(1)
	}

	// Query HLTI API for each GitHub import if token is provided
	if hltiToken != "" && len(githubImports) > 0 {
		fmt.Fprintf(os.Stderr, "Querying HLTI API for %d GitHub packages...\n", len(githubImports))
		client := &http.Client{
			Timeout: 30 * time.Second,
		}
		
		for importPath := range githubImports {
			info, err := queryHLTIAPI(client, hltiAPIURL, hltiToken, importPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to query API for %s: %v\n", importPath, err)
				githubImportResults[importPath] = &PackageInfo{
					ImportPath: importPath,
					Error:      err.Error(),
				}
			} else {
				githubImportResults[importPath] = info
			}
			// Small delay to avoid rate limiting
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Generate report
	fmt.Println("# Go Imports Report")
	fmt.Println("(Standard library and GitHub packages filtered out)")
	fmt.Printf("Generated: %s\n\n", getCurrentTime())

	// Sort file paths
	files := make([]string, 0, len(allImports))
	for file := range allImports {
		files = append(files, file)
	}
	sort.Strings(files)

	// Output imports by file
	totalImports := 0
	for _, file := range files {
		fmt.Printf("## File: %s\n\n", file)
		imports := allImports[file]
		importList := make([]string, 0, len(imports))
		for imp := range imports {
			importList = append(importList, imp)
		}
		sort.Strings(importList)
		for _, imp := range importList {
			fmt.Printf("- `%s`\n", imp)
			totalImports++
		}
		fmt.Println()
	}

	// Summary
	fmt.Println("---")
	fmt.Println()
	fmt.Println("## Summary")
	fmt.Println()
	fmt.Printf("Total third-party imports (excluding stdlib and GitHub): %d\n", totalImports)
	fmt.Printf("Unique third-party imports: %d\n", len(uniqueImports))
	fmt.Printf("GitHub packages found: %d\n", len(githubImports))
	fmt.Printf("Standard library packages found: %d\n", len(standardLibImports))
	fmt.Println()

	// GitHub packages section
	if len(githubImports) > 0 {
		fmt.Println("### GitHub Packages")
		fmt.Println()
		githubList := make([]string, 0, len(githubImports))
		for imp := range githubImports {
			githubList = append(githubList, imp)
		}
		sort.Strings(githubList)
		for _, imp := range githubList {
			fmt.Printf("- `%s`\n", imp)
			if result, exists := githubImportResults[imp]; exists {
				if result.Error != "" {
					fmt.Printf("  - ⚠️  API Error: %s\n", result.Error)
				} else {
					if result.RepositoryID != 0 {
						fmt.Printf("  - Repository ID: %d\n", result.RepositoryID)
					}
					if result.Repository != "" {
						fmt.Printf("  - Repository: %s\n", result.Repository)
					}
					if len(result.GeocodedLocation) > 0 {
						fmt.Printf("  - Geocoded Locations: %d found\n", len(result.GeocodedLocation))
						for _, loc := range result.GeocodedLocation {
							if loc.CountryName != "" {
								fmt.Printf("    - %s (%s)\n", loc.CountryName, loc.ISO3166Alpha2)
							}
						}
					}
				}
			}
		}
		fmt.Println()
	}
	
	// Third-party imports (non-stdlib, non-GitHub)
	if len(uniqueImports) > 0 {
		fmt.Println("### All Unique Third-Party Imports (excluding stdlib and GitHub)")
		fmt.Println()
	uniqueList := make([]string, 0, len(uniqueImports))
	for imp := range uniqueImports {
		uniqueList = append(uniqueList, imp)
	}
	sort.Strings(uniqueList)
	for _, imp := range uniqueList {
		fmt.Printf("- `%s`\n", imp)
		}
	}
}

func getCurrentTime() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05 UTC")
}

// isStandardLibrary checks if an import path is from the Go standard library.
// Standard library packages don't have a dot in the first path segment.
func isStandardLibrary(importPath string) bool {
	// Handle blank imports (like _ "github.com/lib/pq")
	if importPath == "" {
		return false
	}
	
	// Get the first segment of the path
	firstSegment := strings.Split(importPath, "/")[0]
	
	// Standard library packages don't contain dots in the first segment
	// Examples: "fmt", "os", "net/http", "encoding/json"
	// Non-stdlib examples: "github.com/...", "golang.org/...", "google.golang.org/..."
	return !strings.Contains(firstSegment, ".")
}

// isGitHubPackage checks if an import path is from GitHub.
func isGitHubPackage(importPath string) bool {
	return strings.HasPrefix(importPath, "github.com/")
}

// PackageInfo represents the information returned from the HLTI API
type PackageInfo struct {
	ImportPath       string
	RepositoryID     int64
	Repository       string
	GeocodedLocation []GeocodedPkgLocation
	Error            string
}

// GeocodedPkgLocation represents geocoded location data
type GeocodedPkgLocation struct {
	Formatted              string `json:"formatted"`
	CountryName            string `json:"country_name"`
	ISO3166Alpha2          string `json:"iso_3166_alpha_2"`
	ISO3166Alpha3          string `json:"iso_3166_alpha_3"`
	Timestamp              string `json:"timestamp"`
	Reason                 string `json:"reason"`
	Latitude               string `json:"latitude"`
	Longitude              string `json:"longitude"`
	OpenStreetMapURL       string `json:"openstreetmaps_url"`
	Timezone               string `json:"timezone"`
	TimezoneOffset         string `json:"timezone_offset"`
	OrganizationName       string `json:"organization_name"`
	OrganizationDomain     string `json:"organization_domain"`
	OrganizationGitHubRepo string `json:"organization_github_repo"`
}

// queryHLTIAPI queries the HLTI API for package information
func queryHLTIAPI(client *http.Client, apiURL, token, importPath string) (*PackageInfo, error) {
	// For GitHub packages, use "go" as ecosystem and the full import path as package name
	// URL encode the package name
	encodedPackage := url.QueryEscape(importPath)
	apiEndpoint := fmt.Sprintf("%s/package/go/%s?token=%s", strings.TrimSuffix(apiURL, "/"), encodedPackage, url.QueryEscape(token))
	
	req, err := http.NewRequest("GET", apiEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Accept", "application/json")
	
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}
	
	// Parse the JSON response - matches GitHubPackageHistory structure
	var apiResponse struct {
		RepositoryID     int64                `json:"repository_id"`
		Repository       string               `json:"repository"`
		GeocodedLocation []GeocodedPkgLocation `json:"geocoded_location"`
	}
	
	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	
	return &PackageInfo{
		ImportPath:       importPath,
		RepositoryID:     apiResponse.RepositoryID,
		Repository:       apiResponse.Repository,
		GeocodedLocation: apiResponse.GeocodedLocation,
	}, nil
}

