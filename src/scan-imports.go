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
	githubPackageFiles := make(map[string][]string) // package -> []files that use it
	
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

		// Make path relative to root
		relPath, _ := filepath.Rel(rootDir, path)

		// Extract imports
		for _, imp := range node.Imports {
			importPath := strings.Trim(imp.Path.Value, "\"")
			
			// Categorize imports
			if isStandardLibrary(importPath) {
				standardLibImports[importPath] = true
			} else if isGitHubPackage(importPath) {
				githubImports[importPath] = true
				// Track which files use this GitHub package
				githubPackageFiles[importPath] = append(githubPackageFiles[importPath], relPath)
			} else {
				// Only track non-standard, non-GitHub imports
				imports[importPath] = true
				uniqueImports[importPath] = true
			}
		}

		if len(imports) > 0 {
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

	// Calculate FOCI statistics
	fociPresentCount := 0
	totalRepoFoci := 0
	totalUserFoci := 0
	packagesWithErrors := 0
	
	// Output FOCI summary to a file for GitHub Actions summary
	fociSummaryFile := os.Getenv("FOCI_SUMMARY_FILE")
	var fociSummary *os.File
	if fociSummaryFile != "" {
		var err error
		fociSummary, err = os.Create(fociSummaryFile)
		if err == nil {
			defer fociSummary.Close()
		}
	}
	
	for _, result := range githubImportResults {
		if result.Error != "" {
			packagesWithErrors++
		} else {
			if result.FociPresent {
				fociPresentCount++
			}
			totalRepoFoci += len(result.RepositoryFoci)
			totalUserFoci += len(result.UserFoci)
		}
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
	
	// FOCI Summary with detailed information
	if len(githubImportResults) > 0 {
		fmt.Println("### FOCI Analysis")
		fmt.Println()
		fmt.Printf("Packages with FOCI present: %d\n", fociPresentCount)
		fmt.Printf("Total repository FOCI locations: %d\n", totalRepoFoci)
		fmt.Printf("Total user FOCI locations: %d\n", totalUserFoci)
		if packagesWithErrors > 0 {
			fmt.Printf("Packages with API errors: %d\n", packagesWithErrors)
		}
		fmt.Println()
		
		// Detailed FOCI information by package
		githubList := make([]string, 0, len(githubImports))
		for imp := range githubImports {
			githubList = append(githubList, imp)
		}
		sort.Strings(githubList)
		
		// Write FOCI summary for GitHub Actions
		if fociSummary != nil {
			// Summary statistics in table format
			fmt.Fprintf(fociSummary, "<table>\n")
			fmt.Fprintf(fociSummary, "<tr><th>FOCI Metric</th><th>Count</th></tr>\n")

			// Color code based on findings
			fociIcon := "✅"
			if fociPresentCount > 0 {
				fociIcon = "⚠️"
			}

			fmt.Fprintf(fociSummary, "<tr><td>%s Packages with FOCI</td><td><strong>%d</strong></td></tr>\n", fociIcon, fociPresentCount)
			fmt.Fprintf(fociSummary, "<tr><td>📍 Repository FOCI Locations</td><td><strong>%d</strong></td></tr>\n", totalRepoFoci)
			fmt.Fprintf(fociSummary, "<tr><td>👤 User FOCI Locations</td><td><strong>%d</strong></td></tr>\n", totalUserFoci)

			if packagesWithErrors > 0 {
				fmt.Fprintf(fociSummary, "<tr><td>❌ Packages with Errors</td><td><strong>%d</strong></td></tr>\n", packagesWithErrors)
			}

			fmt.Fprintf(fociSummary, "</table>\n\n")

			if fociPresentCount > 0 {
				fmt.Fprintf(fociSummary, "#### 🚨 Packages with FOCI Details\n\n")
			}
		}
		
		for _, imp := range githubList {
			if result, exists := githubImportResults[imp]; exists && result.Error == "" {
				hasFociData := result.FociPresent || len(result.RepositoryFoci) > 0 || len(result.UserFoci) > 0
				if hasFociData {
					// Get files that use this package
					files := githubPackageFiles[imp]
					sort.Strings(files)
					
					fmt.Printf("#### `%s`\n", imp)
					fmt.Println()
					if result.Owner != "" && result.Name != "" {
						fmt.Printf("**Repository:** `%s/%s`\n", result.Owner, result.Name)
					}
					if result.RepositoryID != 0 {
						fmt.Printf("**Repository ID:** %d\n", result.RepositoryID)
					}
					
					// FOCI Status
					if result.FociPresent {
						fmt.Printf("**FOCI Status:** ✅ Present\n")
					} else {
						fmt.Printf("**FOCI Status:** ❌ Not Present\n")
					}
					
					// Repository FOCI
					if len(result.RepositoryFoci) > 0 {
						fmt.Printf("\n**Repository FOCI Locations** (%d):\n", len(result.RepositoryFoci))
						for _, loc := range result.RepositoryFoci {
							if loc.CountryName != "" {
								details := []string{}
								if loc.ISO3166Alpha2 != "" {
									details = append(details, loc.ISO3166Alpha2)
								}
								if loc.OrganizationName != "" {
									details = append(details, fmt.Sprintf("Org: %s", loc.OrganizationName))
								}
								detailStr := ""
								if len(details) > 0 {
									detailStr = " (" + strings.Join(details, ", ") + ")"
								}
								fmt.Printf("- %s%s\n", loc.CountryName, detailStr)
							}
						}
					}
					
					// User FOCI
					if len(result.UserFoci) > 0 {
						fmt.Printf("\n**User FOCI Locations** (%d):\n", len(result.UserFoci))
						for _, loc := range result.UserFoci {
							if loc.CountryName != "" {
								details := []string{}
								if loc.ISO3166Alpha2 != "" {
									details = append(details, loc.ISO3166Alpha2)
								}
								if loc.OrganizationName != "" {
									details = append(details, fmt.Sprintf("Org: %s", loc.OrganizationName))
								}
								detailStr := ""
								if len(details) > 0 {
									detailStr = " (" + strings.Join(details, ", ") + ")"
								}
								fmt.Printf("- %s%s\n", loc.CountryName, detailStr)
							}
						}
					}
					fmt.Println()
					
					// Write to FOCI summary file for GitHub Actions
					if fociSummary != nil && result.FociPresent {
						// Create expandable section for each package
						fmt.Fprintf(fociSummary, "<details>\n")
						fmt.Fprintf(fociSummary, "<summary><strong>📦 <code>%s</code></strong>", imp)

						if result.Owner != "" && result.Name != "" {
							fmt.Fprintf(fociSummary, " - <code>%s/%s</code>", result.Owner, result.Name)
						}
						fmt.Fprintf(fociSummary, "</summary>\n\n")

						// Package details in table format
						fmt.Fprintf(fociSummary, "<table>\n")

						// Files using this package
						if len(files) > 0 {
							fmt.Fprintf(fociSummary, "<tr><td><strong>📄 Used in Files</strong></td><td>%d file(s)</td></tr>\n", len(files))
							fmt.Fprintf(fociSummary, "<tr><td colspan=\"2\">\n")
							for _, file := range files {
								fmt.Fprintf(fociSummary, "• <code>%s</code><br>\n", file)
							}
							fmt.Fprintf(fociSummary, "</td></tr>\n")
						}

						// Repository FOCI locations
						if len(result.RepositoryFoci) > 0 {
							fmt.Fprintf(fociSummary, "<tr><td><strong>📍 Repository FOCI</strong></td><td>%d location(s)</td></tr>\n", len(result.RepositoryFoci))
							fmt.Fprintf(fociSummary, "<tr><td colspan=\"2\">\n")
							for _, loc := range result.RepositoryFoci {
								if loc.CountryName != "" {
									flag := ""
									if loc.ISO3166Alpha2 != "" {
										flag = loc.ISO3166Alpha2
									}
									orgInfo := ""
									if loc.OrganizationName != "" {
										orgInfo = fmt.Sprintf(" - <em>%s</em>", loc.OrganizationName)
									}
									fmt.Fprintf(fociSummary, "🌍 <strong>%s</strong> (%s)%s<br>\n", loc.CountryName, flag, orgInfo)
								}
							}
							fmt.Fprintf(fociSummary, "</td></tr>\n")
						}

						// User FOCI locations
						if len(result.UserFoci) > 0 {
							fmt.Fprintf(fociSummary, "<tr><td><strong>👤 User FOCI</strong></td><td>%d location(s)</td></tr>\n", len(result.UserFoci))
							fmt.Fprintf(fociSummary, "<tr><td colspan=\"2\">\n")
							for _, loc := range result.UserFoci {
								if loc.CountryName != "" {
									flag := ""
									if loc.ISO3166Alpha2 != "" {
										flag = loc.ISO3166Alpha2
									}
									orgInfo := ""
									if loc.OrganizationName != "" {
										orgInfo = fmt.Sprintf(" - <em>%s</em>", loc.OrganizationName)
									}
									fmt.Fprintf(fociSummary, "👥 <strong>%s</strong> (%s)%s<br>\n", loc.CountryName, flag, orgInfo)
								}
							}
							fmt.Fprintf(fociSummary, "</td></tr>\n")
						}

						fmt.Fprintf(fociSummary, "</table>\n")
						fmt.Fprintf(fociSummary, "</details>\n\n")
					}
				}
			}
		}

		// Add error section to FOCI summary
		if fociSummary != nil && packagesWithErrors > 0 {
			fmt.Fprintf(fociSummary, "#### ❌ Packages with API Errors\n\n")
			fmt.Fprintf(fociSummary, "<table>\n")
			fmt.Fprintf(fociSummary, "<tr><th>Package</th><th>Error</th></tr>\n")
			for _, imp := range githubList {
				if result, exists := githubImportResults[imp]; exists && result.Error != "" {
					fmt.Fprintf(fociSummary, "<tr><td><code>%s</code></td><td>⚠️ %s</td></tr>\n", imp, result.Error)
				}
			}
			fmt.Fprintf(fociSummary, "</table>\n\n")
		}

		// List packages with errors
		if packagesWithErrors > 0 {
			fmt.Println("#### Packages with API Errors")
			fmt.Println()
			for _, imp := range githubList {
				if result, exists := githubImportResults[imp]; exists && result.Error != "" {
					fmt.Printf("- `%s`: ⚠️ %s\n", imp, result.Error)
				}
			}
			fmt.Println()
		}
	}

	// GitHub packages section (just list, no FOCI details)
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
	Owner            string
	Name             string
	Package          string
	FociPresent      bool
	RepositoryFoci   []GeocodedPkgLocation
	UserFoci         []GeocodedLocation
	Error            string
}

// GeocodedLocation represents user geocoded location data
type GeocodedLocation struct {
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
	// Use the /foci/present endpoint
	apiEndpoint := fmt.Sprintf("%s/foci/present/go/%s?token=%s", strings.TrimSuffix(apiURL, "/"), encodedPackage, url.QueryEscape(token))
	
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
	
	// Parse the JSON response - GetPackagesFociResponse is a map[string]*PackageFoci
	var apiResponse map[string]*struct {
		RepoID    int64                `json:"repo_id"`
		Owner     string               `json:"owner"`
		Name      string               `json:"name"`
		Package   string               `json:"package"`
		Foci      bool                 `json:"foci"`
		RepoFoci  []GeocodedPkgLocation `json:"repository_foci"`
		UserFoci  []GeocodedLocation   `json:"user_foci"`
	}
	
	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	
	// Extract the package info from the map (key is the package name)
	pkgInfo, exists := apiResponse[importPath]
	if !exists {
		// Try to find any entry in the map (in case the key is slightly different)
		for _, info := range apiResponse {
			pkgInfo = info
			break
		}
		if pkgInfo == nil {
			return nil, fmt.Errorf("package not found in API response")
		}
	}
	
	return &PackageInfo{
		ImportPath:     importPath,
		RepositoryID:   pkgInfo.RepoID,
		Owner:          pkgInfo.Owner,
		Name:           pkgInfo.Name,
		Package:        pkgInfo.Package,
		FociPresent:    pkgInfo.Foci,
		RepositoryFoci: pkgInfo.RepoFoci,
		UserFoci:       pkgInfo.UserFoci,
	}, nil
}


