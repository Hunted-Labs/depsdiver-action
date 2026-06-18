// Command render consumes the JSON output of `diver scan --output json` and 
// produces the DepsDiver markdown report, the GitHub Actions step-summary 
// file, and the action's outputs.
//
// Usage:
//	render <folders-file> <json-dir>
//
// folders-file is a newline-delimited list of the scanned folders (in order);
// json-dir holds diver's JSON output named 0.json, 1.json, etc. paired with each
// folder by line index. This avoids any JSON-escaping of user input in shell.
package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---- diver scan JSON (input) ----

type diverFociStat struct {
	ChangeRatio *float64 `json:"change_ratio"`
	CountryName *string  `json:"country_name"`
	FociPresent bool     `json:"foci_present"`
}

type diverFoci struct {
	Owner          string                `json:"owner"`
	Name           string                `json:"name"`
	Package        string                `json:"package"`
	Foci           bool                  `json:"foci"`
	RepositoryFoci []GeocodedPkgLocation `json:"repository_foci"`
	FociStats      []diverFociStat       `json:"foci_stats"`
}

type diverDep struct {
	Name       string     `json:"name"`
	Version    string     `json:"version"`
	Ecosystem  string     `json:"ecosystem"`
	SourceFile string     `json:"source_file"`
	Suppressed bool       `json:"suppressed"`
	FOCI       *diverFoci `json:"foci"`
}

type diverScan struct {
	ScannedPath  string     `json:"scanned_path"`
	Dependencies []diverDep `json:"dependencies"`
}

type inputEntry struct {
	Folder   string
	JSONFile string
}

type PackageManagerDep struct {
	Name       string
	Version    string
	Ecosystem  string
	SourceFile string
}

type PackageInfo struct {
	Owner          string
	Name           string
	Package        string
	FociPresent    bool
	ChangeRatio    float64
	RepositoryFoci []GeocodedPkgLocation
	FociStats      []FociStat
	Error          string
}

type FociStat struct {
	ChangeRatio float64
	CountryName string
	FociPresent bool
}

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

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <folders-file> <json-dir>\n", os.Args[0])
		os.Exit(1)
	}

	foldersData, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read folders file: %v\n", err)
		os.Exit(1)
	}
	jsonDir := os.Args[2]

	var inputs []inputEntry
	for _, line := range strings.Split(string(foldersData), "\n") {
		folder := strings.TrimSpace(line)
		if folder == "" {
			continue
		}
		inputs = append(inputs, inputEntry{
			Folder:   folder,
			JSONFile: filepath.Join(jsonDir, fmt.Sprintf("%d.json", len(inputs))),
		})
	}

	depsDiverAPIURL := os.Getenv("DEPSDIVER_API_URL")
	if depsDiverAPIURL == "" {
		depsDiverAPIURL = "https://depsdiver.com/api"
	}

	fociThreshold := -1.0
	if thresholdStr := os.Getenv("FOCI_THRESHOLD"); thresholdStr != "" {
		if t, err := strconv.ParseFloat(thresholdStr, 64); err == nil && t >= 0 && t <= 100 {
			fociThreshold = t
		}
	}

	// Build the dependency list + results map from each folder's diver JSON.
	var pkgManagerDeps []PackageManagerDep
	pkgManagerResults := make(map[string]*PackageInfo)
	seenDep := make(map[string]bool)

	for _, in := range inputs {
		data, err := os.ReadFile(in.JSONFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: cannot read %q: %v\n", in.JSONFile, err)
			continue
		}
		var scan diverScan
		if err := json.Unmarshal(data, &scan); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: cannot parse %q: %v\n", in.JSONFile, err)
			continue
		}

		for _, d := range scan.Dependencies {
			if d.Suppressed {
				continue
			}
			// Re-apply the folder prefix diver strips (its source_file is
			// relative to the scanned folder).
			source := d.SourceFile
			if in.Folder != "" && in.Folder != "." {
				source = filepath.Join(in.Folder, d.SourceFile)
			}

			key := d.Ecosystem + ":" + d.Name
			if !seenDep[key] {
				seenDep[key] = true
				pkgManagerDeps = append(pkgManagerDeps, PackageManagerDep{
					Name:       d.Name,
					Version:    d.Version,
					Ecosystem:  d.Ecosystem,
					SourceFile: source,
				})
			}

			if _, ok := pkgManagerResults[key]; ok {
				continue
			}
			pkgManagerResults[key] = toPackageInfo(d)
		}
	}

	renderReport(pkgManagerDeps, pkgManagerResults, fociThreshold, depsDiverAPIURL)
}

// toPackageInfo converts a diver dependency's raw foci object into the
// PackageInfo the report code expects. A nil foci object means diver got no
// FOCI record for the package — treated as "no data available".
func toPackageInfo(d diverDep) *PackageInfo {
	if d.FOCI == nil {
		return &PackageInfo{Name: d.Name, Error: "package not found in API response"}
	}
	var changeRatio float64
	var stats []FociStat
	for _, s := range d.FOCI.FociStats {
		cn := ""
		if s.CountryName != nil {
			cn = *s.CountryName
		}
		cr := 0.0
		if s.ChangeRatio != nil {
			cr = *s.ChangeRatio
		}
		if s.FociPresent {
			changeRatio += cr
		}
		stats = append(stats, FociStat{ChangeRatio: cr, CountryName: cn, FociPresent: s.FociPresent})
	}
	return &PackageInfo{
		Owner:          d.FOCI.Owner,
		Name:           d.FOCI.Name,
		Package:        d.FOCI.Package,
		FociPresent:    d.FOCI.Foci,
		ChangeRatio:    changeRatio,
		RepositoryFoci: d.FOCI.RepositoryFoci,
		FociStats:      stats,
	}
}

func renderReport(pkgManagerDeps []PackageManagerDep, pkgManagerResults map[string]*PackageInfo, fociThreshold float64, depsDiverAPIURL string) {
	fociPresentCount := 0
	totalRepoFoci := 0
	packagesNotFound := 0
	packagesWithErrors := 0

	fociSummaryFile := os.Getenv("FOCI_SUMMARY_FILE")
	var fociSummary *os.File
	if fociSummaryFile != "" {
		var err error
		fociSummary, err = os.Create(fociSummaryFile)
		if err == nil {
			defer fociSummary.Close()
		}
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

	fmt.Println("# Dependency FOCI Report")
	fmt.Printf("Generated: %s\n\n", getCurrentTime())
	fmt.Println("## Summary")
	fmt.Println()

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

	if fociSummary != nil && len(pkgManagerDeps) > 0 {
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
			fmt.Printf("No data available: %d\n", packagesNotFound)
		}
		if packagesWithErrors > 0 {
			fmt.Printf("API errors: %d\n", packagesWithErrors)
		}
		fmt.Printf("Total repository FOCI locations: %d\n", totalRepoFoci)
		fmt.Println()

		if fociSummary != nil {
			fmt.Fprintf(fociSummary, "**Results:** %d passed · %d FOCI detected", passedCount, fociPresentCount)
			if packagesNotFound > 0 {
				fmt.Fprintf(fociSummary, " · %d no data available", packagesNotFound)
			}
			fmt.Fprintf(fociSummary, "\n\n")
			writeFociTriageTable(fociSummary, pkgManagerDeps, pkgManagerResults, fociThreshold, depsDiverAPIURL)
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
			fmt.Printf("**Total Foreign Contribution:** %s\n\n", formatPct(result.ChangeRatio))

			if result.ChangeRatio*100 > multiCountryCutoff {
				fmt.Printf("> **Note:** %s\n\n", multiCountryDisclaimer)
			}

			if len(result.FociStats) > 0 {
				fmt.Println("**Countries of Concern:**")
				for _, stat := range result.FociStats {
					if stat.FociPresent && stat.CountryName != "" {
						fmt.Printf("- %s — %s\n", stat.CountryName, formatPct(stat.ChangeRatio))
					}
				}
				fmt.Println()
			}

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
					fmt.Printf("- `%s` ⚠️ FOCI detected (%s)\n", dep.Name, formatPct(result.ChangeRatio))
				} else if result.Error != "" {
					if isNotFound(result.Error) {
						fmt.Printf("- `%s` (no data available)\n", dep.Name)
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

	if fociThreshold >= 0 && len(pkgManagerResults) > 0 {
		fmt.Println("---")
		fmt.Println()
		fmt.Println("## FOCI Threshold Summary")
		fmt.Println()
		fmt.Printf("Threshold: %.0f%% change ratio\n", fociThreshold)
		fmt.Printf("Packages above threshold: %d\n", fociPresentCount)
	}
}

// multiCountryCutoff is the total foreign-contribution percentage above which
// we surface the multi-country disclaimer. It sits just above 100% so ordinary
// rounding noise at exactly 100% doesn't trigger it.
const multiCountryCutoff = 100.05

// multiCountryDisclaimer explains why a package's total foreign contribution can
// exceed 100% (a contributor associated with more than one country is counted
// for each). Mirrors the disclaimer shown in the DepsDiver UI.
const multiCountryDisclaimer = "Some users are associated with multiple countries, making it impossible to definitively attribute their contributions to a single location. This can occur when users live in one country and work remotely for another, or when they frequently travel between countries. To provide the most comprehensive view of activity, contributions are counted for all relevant countries, which may cause country-level percentages to exceed 100% in some cases."

// renders the FOCI-flagged packages as a single table, sorted by
// foreign contribution (highest first), remainder go in a collapsed
// "view more" section
func writeFociTriageTable(w *os.File, pkgManagerDeps []PackageManagerDep, results map[string]*PackageInfo, fociThreshold float64, apiURL string) {
	const triageTopN = 30

	type fociRow struct {
		dep    PackageManagerDep
		result *PackageInfo
	}
	var rows []fociRow
	for _, dep := range pkgManagerDeps {
		key := dep.Ecosystem + ":" + dep.Name
		result, ok := results[key]
		if !ok || result.Error != "" {
			continue
		}
		isFoci := result.FociPresent
		if fociThreshold >= 0 {
			isFoci = result.ChangeRatio*100 > fociThreshold
		}
		if isFoci {
			rows = append(rows, fociRow{dep, result})
		}
	}
	if len(rows) == 0 {
		return
	}

	// Highest contribution first
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].result.ChangeRatio > rows[j].result.ChangeRatio
	})

	// rows are sorted highest-first, so any package over 100% is at the very top.
	anyOverHundred := rows[0].result.ChangeRatio*100 > multiCountryCutoff

	writeRow := func(r fociRow) {
		pctText := formatPct(r.result.ChangeRatio)
		if r.result.ChangeRatio*100 > multiCountryCutoff {
			pctText += " *"
		}
		encoded := url.QueryEscape(r.dep.Name)
		reportURL := fmt.Sprintf("%s/analyze/%s?ecosystem=%s#overview", strings.TrimSuffix(apiURL, "/api"), encoded, r.dep.Ecosystem)
		repo := "—"
		if r.result.Owner != "" && r.result.Name != "" {
			repo = fmt.Sprintf("<code>%s/%s</code>", r.result.Owner, r.result.Name)
		}
		fmt.Fprintf(w, "<tr><td><a href=\"%s\"><code>%s</code></a></td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>\n",
			reportURL, r.dep.Name, r.dep.Ecosystem, pctText, fociCountries(r.result), repo)
	}

	header := "<table>\n<tr><th>Package</th><th>Ecosystem</th><th>Foreign contribution</th><th>FOCI countries</th><th>Repository</th></tr>\n"

	// Disclaimer goes ABOVE the table: the >100% packages sort to the top, so the
	// explanation has to be the first thing the reader sees, not buried below.
	if anyOverHundred {
		fmt.Fprintf(w, "<blockquote>⚠️ <strong>Some percentages exceed 100%% (marked with * below):</strong> %s</blockquote>\n\n", multiCountryDisclaimer)
	}

	fmt.Fprintf(w, "<p><strong>⚠️ Dependencies with foreign contribution</strong> (highest first)</p>\n\n")
	fmt.Fprintf(w, "%s", header)
	top := rows
	if len(top) > triageTopN {
		top = rows[:triageTopN]
	}
	for _, r := range top {
		writeRow(r)
	}
	fmt.Fprintf(w, "</table>\n\n")

	if len(rows) > triageTopN {
		fmt.Fprintf(w, "<details>\n<summary>View remaining %d FOCI packages</summary>\n\n", len(rows)-triageTopN)
		fmt.Fprintf(w, "%s", header)
		for _, r := range rows[triageTopN:] {
			writeRow(r)
		}
		fmt.Fprintf(w, "</table>\n\n</details>\n\n")
	}
}

// fociCountries formats the FOCI-flagged countries for a package as a compact
// "Country pct, …" string for the triage table, highest contribution first.
// Caps at maxShown countries with a "+N more" suffix to keep cells readable.
func fociCountries(result *PackageInfo) string {
	const maxShown = 3
	type countryContribution struct {
		name  string
		ratio float64
	}
	var ccs []countryContribution
	for _, s := range result.FociStats {
		if s.FociPresent && s.CountryName != "" {
			ccs = append(ccs, countryContribution{s.CountryName, s.ChangeRatio})
		}
	}
	if len(ccs) == 0 {
		return "—"
	}
	sort.SliceStable(ccs, func(i, j int) bool { return ccs[i].ratio > ccs[j].ratio })

	var parts []string
	for i, c := range ccs {
		if i >= maxShown {
			break
		}
		parts = append(parts, fmt.Sprintf("%s %s", c.name, formatPct(c.ratio)))
	}
	out := strings.Join(parts, ", ")
	if len(ccs) > maxShown {
		out += fmt.Sprintf(", +%d more", len(ccs)-maxShown)
	}
	return out
}

func getCurrentTime() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05 UTC")
}

func isNotFound(errStr string) bool {
	return strings.Contains(errStr, "status 404") || strings.Contains(errStr, "package not found in API response")
}

func formatPct(ratio float64) string {
	pct := ratio * 100
	if pct > 0 && pct < 0.1 {
		return "<0.1%"
	}
	return fmt.Sprintf("%.1f%%", pct)
}
