package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <directory>\n", os.Args[0])
		os.Exit(1)
	}

	rootDir := os.Args[1]

	// Get DepsDiver API configuration from environment
	depsDiverToken := os.Getenv("DEPSDIVER_TOKEN")
	depsDiverAPIURL := os.Getenv("DEPSDIVER_API_URL")

	// Get FOCI threshold (-1 means disabled; otherwise 0-100 percent)
	fociThreshold := -1.0
	if thresholdStr := os.Getenv("FOCI_THRESHOLD"); thresholdStr != "" {
		if t, err := strconv.ParseFloat(thresholdStr, 64); err == nil && t >= 0 && t <= 100 {
			fociThreshold = t
		}
	}
	if depsDiverAPIURL == "" {
		depsDiverAPIURL = "https://api.example.com" // default, should be overridden
	}

	pkgManagerDeps, _ := scanPackageManagerFiles(rootDir)
	pkgManagerDeps = dedupePkgManagerDeps(pkgManagerDeps)

	pkgManagerResults := make(map[string]*PackageInfo)
	apiClient := &http.Client{Timeout: 30 * time.Second}

	// Load cache if available
	cacheFile := os.Getenv("DEPSDIVER_CACHE_FILE")
	if cacheFile != "" {
		if data, err := os.ReadFile(cacheFile); err == nil {
			var cached map[string]*PackageInfo
			if err := json.Unmarshal(data, &cached); err == nil {
				for k, v := range cached {
					pkgManagerResults[k] = v
				}
				fmt.Fprintf(os.Stderr, "Loaded %d cached results\n", len(cached))
			}
		}
	}

	// Only query packages not already in cache
	var uncachedDeps []PackageManagerDep
	for _, dep := range pkgManagerDeps {
		key := dep.Ecosystem + ":" + dep.Name
		if _, cached := pkgManagerResults[key]; !cached {
			uncachedDeps = append(uncachedDeps, dep)
		}
	}

	if depsDiverToken != "" && len(uncachedDeps) > 0 {
		fmt.Fprintf(os.Stderr, "Querying DepsDiver API for %d packages (%d cached)...\n", len(uncachedDeps), len(pkgManagerDeps)-len(uncachedDeps))

		// Bulk query in chunks of 20
		const chunkSize = 20
		for i := 0; i < len(uncachedDeps); i += chunkSize {
			end := i + chunkSize
			if end > len(uncachedDeps) {
				end = len(uncachedDeps)
			}
			chunk := uncachedDeps[i:end]

			bulkResults, err := queryDepsDiverAPIBulk(apiClient, depsDiverAPIURL, depsDiverToken, chunk)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Bulk query failed, falling back to individual: %v\n", err)
				bulkResults = map[string]*PackageInfo{}
			}

			// Merge bulk results
			for _, dep := range chunk {
				key := dep.Ecosystem + ":" + dep.Name
				normalizedName := strings.ToLower(dep.Name)
				if info, ok := bulkResults[normalizedName]; ok {
					pkgManagerResults[key] = info
				} else if info, ok := bulkResults[dep.Name]; ok {
					pkgManagerResults[key] = info
				} else {
					// fall back to individual call
					info, err := queryDepsDiverAPI(apiClient, depsDiverAPIURL, depsDiverToken, dep.Name, dep.Ecosystem)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Warning: Failed to query API for %s: %v\n", dep.Name, err)
						pkgManagerResults[key] = &PackageInfo{ImportPath: dep.Name, Error: err.Error()}
					} else {
						pkgManagerResults[key] = info
					}
					time.Sleep(100 * time.Millisecond)
				}
			}
		}

		// Save updated cache
		if cacheFile != "" {
			if data, err := json.Marshal(pkgManagerResults); err == nil {
				if err := os.MkdirAll(filepath.Dir(cacheFile), 0755); err == nil {
					os.WriteFile(cacheFile, data, 0644)
				}
			}
		}
	}

	// Calculate FOCI statistics
	fociPresentCount := 0
	totalRepoFoci := 0
	packagesNotFound := 0
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

	isNotFound := func(errStr string) bool {
		return strings.Contains(errStr, "status 404") || strings.Contains(errStr, "package not found in API response")
	}

	tallyResult := func(result *PackageInfo) {
		if result.Error != "" {
			if isNotFound(result.Error) {
				packagesNotFound++
			} else {
				packagesWithErrors++
			}
			return
		}
		if fociThreshold >= 0 {
			if result.ChangeRatio*100 > fociThreshold {
				fociPresentCount++
			}
		} else if result.FociPresent {
			fociPresentCount++
		}
		totalRepoFoci += len(result.RepositoryFoci)
	}

	for _, result := range pkgManagerResults {
		tallyResult(result)
	}

	// Build files-scanned
	fileDepCount := make(map[string]int)
	var fileOrder []string
	seenFiles := make(map[string]bool)
	for _, dep := range pkgManagerDeps {
		if !seenFiles[dep.SourceFile] {
			seenFiles[dep.SourceFile] = true
			fileOrder = append(fileOrder, dep.SourceFile)
		}
		fileDepCount[dep.SourceFile]++
	}

	passedCount := len(pkgManagerResults) - fociPresentCount - packagesNotFound - packagesWithErrors

	// Generate report
	fmt.Println("# Dependency FOCI Report")
	fmt.Printf("Generated: %s\n\n", getCurrentTime())

	fmt.Println("## Summary")
	fmt.Println()

	// Files scanned
	if len(fileOrder) > 0 {
		fmt.Println("### Files Scanned")
		fmt.Println()
		for _, f := range fileOrder {
			fmt.Printf("- `%s` (%d packages)\n", f, fileDepCount[f])
		}
		fmt.Println()
	}

	fmt.Printf("Package manager dependencies found: %d\n", len(pkgManagerDeps))
	fmt.Println()

	// Always write files scanned + all packages to step summary, regardless of API results
	if fociSummary != nil && len(pkgManagerDeps) > 0 {
		// Files scanned table
		if len(fileOrder) > 0 {
			fmt.Fprintf(fociSummary, "<details>\n")
			fmt.Fprintf(fociSummary, "<summary><strong>📂 Files Scanned (%d files, %d packages)</strong></summary>\n\n", len(fileOrder), len(pkgManagerDeps))
			fmt.Fprintf(fociSummary, "<table>\n<tr><th>File</th><th>Packages</th></tr>\n")
			for _, f := range fileOrder {
				fmt.Fprintf(fociSummary, "<tr><td><code>%s</code></td><td>%d</td></tr>\n", f, fileDepCount[f])
			}
			fmt.Fprintf(fociSummary, "</table>\n\n")
			fmt.Fprintf(fociSummary, "</details>\n\n")
		}

		// All packages scanned, grouped by ecosystem
		byEcoSummary := make(map[string][]PackageManagerDep)
		var ecoOrderSummary []string
		seenEcoSummary := make(map[string]bool)
		for _, dep := range pkgManagerDeps {
			if !seenEcoSummary[dep.Ecosystem] {
				seenEcoSummary[dep.Ecosystem] = true
				ecoOrderSummary = append(ecoOrderSummary, dep.Ecosystem)
			}
			byEcoSummary[dep.Ecosystem] = append(byEcoSummary[dep.Ecosystem], dep)
		}
		fmt.Fprintf(fociSummary, "<details>\n")
		fmt.Fprintf(fociSummary, "<summary><strong>📦 All Packages Scanned (%d)</strong></summary>\n\n", len(pkgManagerDeps))
		for _, eco := range ecoOrderSummary {
			fmt.Fprintf(fociSummary, "<p><strong>%s</strong></p>\n<ul>\n", eco)
			for _, dep := range byEcoSummary[eco] {
				key := dep.Ecosystem + ":" + dep.Name
				status := "—"
				if result, exists := pkgManagerResults[key]; exists {
					if result.Error != "" {
						if !isNotFound(result.Error) {
							status = "❌"
						}
					} else {
						hasFoci := false
						if fociThreshold >= 0 {
							hasFoci = result.ChangeRatio*100 > fociThreshold
						} else {
							hasFoci = result.FociPresent
						}
						if hasFoci {
							status = "⚠️"
						} else {
							status = "✅"
						}
					}
				}
				fmt.Fprintf(fociSummary, "<li>%s <code>%s</code></li>\n", status, dep.Name)
			}
			fmt.Fprintf(fociSummary, "</ul>\n")
		}
		fmt.Fprintf(fociSummary, "</details>\n\n")
	}

	if len(pkgManagerResults) > 0 {
		fmt.Println("### FOCI Analysis")
		fmt.Println()
		fmt.Printf("Passed: %d\n", passedCount)
		fmt.Printf("FOCI detected: %d\n", fociPresentCount)
		if packagesNotFound > 0 {
			fmt.Printf("Not in DepsDiver database: %d\n", packagesNotFound)
		}
		if packagesWithErrors > 0 {
			fmt.Printf("API errors: %d\n", packagesWithErrors)
		}
		fmt.Printf("Total repository FOCI locations: %d\n", totalRepoFoci)
		fmt.Println()

		if fociSummary != nil {
			fmt.Fprintf(fociSummary, "**Results:** %d passed · %d FOCI detected", passedCount, fociPresentCount)
			if packagesNotFound > 0 {
				fmt.Fprintf(fociSummary, " · %d not in DepsDiver DB", packagesNotFound)
			}
			fmt.Fprintf(fociSummary, "\n\n")
		}

		for _, dep := range pkgManagerDeps {
			key := dep.Ecosystem + ":" + dep.Name
			result, exists := pkgManagerResults[key]
			if !exists || result.Error != "" {
				continue
			}

			var hasFociData bool
			if fociThreshold >= 0 {
				hasFociData = result.ChangeRatio*100 > fociThreshold
			} else {
				hasFociData = result.FociPresent || len(result.RepositoryFoci) > 0
			}
			if !hasFociData {
				continue
			}

			encodedPackage := url.QueryEscape(dep.Name)
			baseURL := strings.TrimSuffix(depsDiverAPIURL, "/api")
			reportURL := fmt.Sprintf("%s/analyze/%s?ecosystem=%s#overview", baseURL, encodedPackage, dep.Ecosystem)

			fmt.Printf("#### `%s` (%s)\n\n", dep.Name, dep.Ecosystem)
			fmt.Printf("**🔗 [View Full Report on Hunted Labs](%s)**\n\n", reportURL)
			if result.Owner != "" && result.Name != "" {
				fmt.Printf("**Repository:** `%s/%s`\n", result.Owner, result.Name)
			}

			fmt.Printf("**Total Foreign Contribution:** %.1f%%\n\n", result.ChangeRatio*100)

			// Countries of concern from foci_stats
			if len(result.FociStats) > 0 {
				fmt.Println("**Countries of Concern:**")
				for _, stat := range result.FociStats {
					if stat.FociPresent && stat.CountryName != "" {
						fmt.Printf("- %s — %.1f%%\n", stat.CountryName, stat.ChangeRatio*100)
					}
				}
				fmt.Println()
			}

			// Repository FOCI
			if len(result.RepositoryFoci) > 0 {
				fmt.Printf("**Repository FOCI (%d):**\n", len(result.RepositoryFoci))
				for _, loc := range result.RepositoryFoci {
					if loc.CountryName != "" {
						line := loc.CountryName
						if loc.OrganizationName != "" {
							line += fmt.Sprintf(" — %s", loc.OrganizationName)
						}
						if loc.Reason != "" {
							line += fmt.Sprintf(" _(%s)_", loc.Reason)
						}
						fmt.Printf("- %s\n", line)
					}
				}
				fmt.Println()
			}

			fmt.Println()

			// Write to FOCI summary file for GitHub Actions
			if fociSummary != nil && result.FociPresent {
				encodedPackageHTML := url.QueryEscape(dep.Name)
				baseURLHTML := strings.TrimSuffix(depsDiverAPIURL, "/api")
				reportURLHTML := fmt.Sprintf("%s/analyze/%s?ecosystem=%s#overview", baseURLHTML, encodedPackageHTML, dep.Ecosystem)

				fmt.Fprintf(fociSummary, "<details>\n")
				fmt.Fprintf(fociSummary, "<summary><strong><code>%s</code></strong> (%s)", dep.Name, dep.Ecosystem)
				if result.Owner != "" && result.Name != "" {
					fmt.Fprintf(fociSummary, " — <code>%s/%s</code>", result.Owner, result.Name)
				}
				fmt.Fprintf(fociSummary, " — %.1f%% foreign contribution</summary>\n\n", result.ChangeRatio*100)
				fmt.Fprintf(fociSummary, "<p>🔗 <a href=\"%s\"><strong>View Full Report on Hunted Labs</strong></a></p>\n\n", reportURLHTML)

				if len(result.FociStats) > 0 {
					fmt.Fprintf(fociSummary, "<table>\n<tr><th>Country</th><th>Contribution</th><th>Risk</th></tr>\n")
					for _, stat := range result.FociStats {
						if stat.FociPresent && stat.CountryName != "" {
							fmt.Fprintf(fociSummary, "<tr><td>%s</td><td>%.1f%%</td><td>⚠️ FOCI</td></tr>\n", stat.CountryName, stat.ChangeRatio*100)
						}
					}
					fmt.Fprintf(fociSummary, "</table>\n\n")
				}

				if len(result.RepositoryFoci) > 0 {
					fmt.Fprintf(fociSummary, "<p><strong>Repository FOCI (%d):</strong></p>\n<ul>\n", len(result.RepositoryFoci))
					for _, loc := range result.RepositoryFoci {
						if loc.CountryName != "" {
							line := fmt.Sprintf("<strong>%s</strong>", loc.CountryName)
							if loc.OrganizationName != "" {
								line += fmt.Sprintf(" — %s", loc.OrganizationName)
							}
							if loc.Reason != "" {
								line += fmt.Sprintf(" <em>(%s)</em>", loc.Reason)
							}
							fmt.Fprintf(fociSummary, "<li>%s</li>\n", line)
						}
					}
					fmt.Fprintf(fociSummary, "</ul>\n\n")
				}

				fmt.Fprintf(fociSummary, "</details>\n\n")
			}
		}

		// Error section — only real errors, not "not found"
		if packagesWithErrors > 0 {
			fmt.Println("#### API Query Errors")
			fmt.Println()
			for _, dep := range pkgManagerDeps {
				key := dep.Ecosystem + ":" + dep.Name
				if result, exists := pkgManagerResults[key]; exists && result.Error != "" && !isNotFound(result.Error) {
					fmt.Printf("- `%s` (%s): %s\n", dep.Name, dep.Ecosystem, result.Error)
				}
			}
			fmt.Println()

			if fociSummary != nil {
				fmt.Fprintf(fociSummary, "### API Query Errors\n\n")
				fmt.Fprintf(fociSummary, "<details>\n")
				fmt.Fprintf(fociSummary, "<summary><strong>View Package Query Errors</strong> (%d packages)</summary>\n\n", packagesWithErrors)
				fmt.Fprintf(fociSummary, "<table>\n")
				fmt.Fprintf(fociSummary, "<tr><th>Package</th><th>Ecosystem</th><th>Error Message</th></tr>\n")
				for _, dep := range pkgManagerDeps {
					key := dep.Ecosystem + ":" + dep.Name
					if result, exists := pkgManagerResults[key]; exists && result.Error != "" && !isNotFound(result.Error) {
						fmt.Fprintf(fociSummary, "<tr><td><code>%s</code></td><td>%s</td><td>%s</td></tr>\n", dep.Name, dep.Ecosystem, result.Error)
					}
				}
				fmt.Fprintf(fociSummary, "</table>\n\n")
				fmt.Fprintf(fociSummary, "</details>\n\n")
			}
		}
	}

	// dependencies grouped by ecosystem
	byEco := make(map[string][]PackageManagerDep)
	for _, dep := range pkgManagerDeps {
		byEco[dep.Ecosystem] = append(byEco[dep.Ecosystem], dep)
	}
	ecoList := make([]string, 0, len(byEco))
	for eco := range byEco {
		ecoList = append(ecoList, eco)
	}
	sort.Strings(ecoList)

	fmt.Println("### Package Manager Dependencies")
	fmt.Println()
	for _, eco := range ecoList {
		pkgs := byEco[eco]
		fmt.Printf("#### %s (%d packages)\n\n", eco, len(pkgs))
		for _, dep := range pkgs {
			key := eco + ":" + dep.Name
			if result, queried := pkgManagerResults[key]; queried {
				hasFoci := false
				if result.Error == "" {
					if fociThreshold >= 0 {
						hasFoci = result.ChangeRatio*100 > fociThreshold
					} else {
						hasFoci = result.FociPresent
					}
				}
				if hasFoci {
					fmt.Printf("- `%s` ⚠️ FOCI detected (%.1f%%)\n", dep.Name, result.ChangeRatio*100)
				} else if result.Error != "" {
				if isNotFound(result.Error) {
					fmt.Printf("- `%s` (not in DepsDiver database)\n", dep.Name)
				} else {
					fmt.Printf("- `%s` (API error: %s)\n", dep.Name, result.Error)
				}
				} else {
					fmt.Printf("- `%s`\n", dep.Name)
				}
			} else {
				fmt.Printf("- `%s`\n", dep.Name)
			}
		}
		fmt.Println()
	}

	// FOCI threshold summary
	if fociThreshold >= 0 && len(pkgManagerResults) > 0 {
		fmt.Println("---")
		fmt.Println()
		fmt.Println("## FOCI Threshold Summary")
		fmt.Println()
		fmt.Printf("Threshold: %.0f%% change ratio\n", fociThreshold)
		fmt.Printf("Packages above threshold: %d\n", fociPresentCount)
	}
}

func getCurrentTime() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05 UTC")
}

// PackageInfo represents the information returned from the DepsDiver API
type PackageInfo struct {
	ImportPath     string
	Ecosystem      string
	RepositoryID   int64
	Owner          string
	Name           string
	Package        string
	FociPresent    bool
	ChangeRatio    float64 // 0.0-1.0: fraction of changes from FOCI-linked contributors
	RepositoryFoci []GeocodedPkgLocation
	FociStats      []FociStat
	Error          string
}

// per-country FOCI contribution data from foci_stats
type FociStat struct {
	ChangeRatio float64
	CountryName string
	FociPresent bool
}

// geocoded location data
type GeocodedPkgLocation struct {
	Formatted              string `json:"Formatted"`
	CountryName            string `json:"CountryName"`
	ISO3166Alpha2          string `json:"ISO3166Alpha2"`
	ISO3166Alpha3          string `json:"ISO3166Alpha3"`
	Timestamp              string `json:"Timestamp"`
	Reason                 string `json:"Reason"`
	Latitude               string `json:"Latitude"`
	Longitude              string `json:"Longitude"`
	OpenStreetMapURL       string `json:"OpenStreetMapURL"`
	Timezone               string `json:"Timezone"`
	TimezoneOffset         string `json:"TimezoneOffset"`
	OrganizationName       string `json:"OrganizationName"`
	OrganizationDomain     string `json:"OrganizationDomain"`
	OrganizationGitHubRepo string `json:"OrganizationGitHubRepo"`
}

// input shape for the bulk endpoint
type packageRequest struct {
	PackageName   string `json:"packageName"`
	PackageSystem string `json:"packageSystem"`
}

// queries POST /foci/present with up to ~20 packages at once
// Returns a map keyed by package name
func queryDepsDiverAPIBulk(client *http.Client, apiURL, token string, deps []PackageManagerDep) (map[string]*PackageInfo, error) {
	body := make(map[string]packageRequest, len(deps))
	for i, dep := range deps {
		body[fmt.Sprintf("pkg_%d", i)] = packageRequest{
			PackageName:   dep.Name,
			PackageSystem: dep.Ecosystem,
		}
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal bulk request: %w", err)
	}

	apiEndpoint := fmt.Sprintf("%s/foci/present", strings.TrimSuffix(apiURL, "/"))
	req, err := http.NewRequest("POST", apiEndpoint, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResponse map[string]*struct {
		RepoID    int64                 `json:"repo_id"`
		Owner     string                `json:"owner"`
		Name      string                `json:"name"`
		Package   string                `json:"package"`
		Foci      bool                  `json:"foci"`
		RepoFoci  []GeocodedPkgLocation `json:"repository_foci"`
		FociStats []struct {
			ChangeRatio float64 `json:"change_ratio"`
			CountryName *string `json:"country_name"`
			FociPresent bool    `json:"foci_present"`
		} `json:"foci_stats"`
	}
	if err := json.Unmarshal(respBody, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	results := make(map[string]*PackageInfo, len(apiResponse))
	for key, pkgInfo := range apiResponse {
		var fociChangeRatio float64
		var fociStats []FociStat
		for _, stat := range pkgInfo.FociStats {
			cn := ""
			if stat.CountryName != nil {
				cn = *stat.CountryName
			}
			if stat.FociPresent {
				fociChangeRatio += stat.ChangeRatio
			}
			fociStats = append(fociStats, FociStat{
				ChangeRatio: stat.ChangeRatio,
				CountryName: cn,
				FociPresent: stat.FociPresent,
			})
		}
		results[key] = &PackageInfo{
			ImportPath:     key,
			RepositoryID:   pkgInfo.RepoID,
			Owner:          pkgInfo.Owner,
			Name:           pkgInfo.Name,
			Package:        pkgInfo.Package,
			FociPresent:    pkgInfo.Foci,
			ChangeRatio:    fociChangeRatio,
			RepositoryFoci: pkgInfo.RepoFoci,
			FociStats:      fociStats,
		}
	}
	return results, nil
}

func queryDepsDiverAPI(client *http.Client, apiURL, token, importPath, ecosystem string) (*PackageInfo, error) {
	encodedPackage := url.QueryEscape(importPath)
	apiEndpoint := fmt.Sprintf("%s/foci/present/%s/%s", strings.TrimSuffix(apiURL, "/"), ecosystem, encodedPackage)

	req, err := http.NewRequest("GET", apiEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

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

	var apiResponse map[string]*struct {
		RepoID    int64                 `json:"repo_id"`
		Owner     string                `json:"owner"`
		Name      string                `json:"name"`
		Package   string                `json:"package"`
		Foci      bool                  `json:"foci"`
		RepoFoci  []GeocodedPkgLocation `json:"repository_foci"`
		FociStats []struct {
			ChangeRatio float64 `json:"change_ratio"`
			CountryName *string `json:"country_name"`
			FociPresent bool    `json:"foci_present"`
		} `json:"foci_stats"`
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	pkgInfo, exists := apiResponse[importPath]
	if !exists {
		for _, info := range apiResponse {
			pkgInfo = info
			break
		}
		if pkgInfo == nil {
			return nil, fmt.Errorf("package not found in API response")
		}
	}

	var fociChangeRatio float64
	var fociStats []FociStat
	for _, stat := range pkgInfo.FociStats {
		cn := ""
		if stat.CountryName != nil {
			cn = *stat.CountryName
		}
		if stat.FociPresent {
			fociChangeRatio += stat.ChangeRatio
		}
		fociStats = append(fociStats, FociStat{
			ChangeRatio: stat.ChangeRatio,
			CountryName: cn,
			FociPresent: stat.FociPresent,
		})
	}

	return &PackageInfo{
		ImportPath:     importPath,
		RepositoryID:   pkgInfo.RepoID,
		Owner:          pkgInfo.Owner,
		Name:           pkgInfo.Name,
		Package:        pkgInfo.Package,
		FociPresent:    pkgInfo.Foci,
		ChangeRatio:    fociChangeRatio,
		RepositoryFoci: pkgInfo.RepoFoci,
		FociStats:      fociStats,
	}, nil
}

