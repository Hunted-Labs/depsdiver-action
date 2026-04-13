package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Dependency struct {
	Name       string         `json:"name"`
	Version    string         `json:"version,omitempty"`
	Ecosystem  string         `json:"ecosystem"`
	SourceFile string         `json:"source_file"`
	Suppressed bool           `json:"suppressed"`
	FOCI       map[string]any `json:"foci,omitempty"`
}

type ScanResult struct {
	ScannedPath  string       `json:"scanned_path"`
	Dependencies []Dependency `json:"dependencies"`
}

type FociStat struct {
	ChangeRatio float64
	CountryName string
	FociPresent bool
}

type RepoFociLoc map[string]string

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <diver-json-file>\n", os.Args[0])
		os.Exit(1)
	}

	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	var result ScanResult
	if err := json.Unmarshal(data, &result); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing JSON: %v\n", err)
		os.Exit(1)
	}

	depsDiverAPIURL := os.Getenv("DEPSDIVER_API_URL")
	fociThreshold := -1.0
	if thresholdStr := os.Getenv("FOCI_THRESHOLD"); thresholdStr != "" {
		if t, err := strconv.ParseFloat(thresholdStr, 64); err == nil && t >= 0 && t <= 100 {
			fociThreshold = t
		}
	}

	fociSummaryFile := os.Getenv("FOCI_SUMMARY_FILE")
	var fociSummary *os.File
	if fociSummaryFile != "" {
		fociSummary, _ = os.Create(fociSummaryFile)
		if fociSummary != nil {
			defer fociSummary.Close()
		}
	}

	deps := result.Dependencies

	// Build files-scanned map (non-suppressed only)
	fileDepCount := make(map[string]int)
	var fileOrder []string
	seenFiles := make(map[string]bool)
	for _, dep := range deps {
		if dep.Suppressed {
			continue
		}
		if !seenFiles[dep.SourceFile] {
			seenFiles[dep.SourceFile] = true
			fileOrder = append(fileOrder, dep.SourceFile)
		}
		fileDepCount[dep.SourceFile]++
	}

	// Count active (non-suppressed) dependencies
	activeDeps := 0
	for _, dep := range deps {
		if !dep.Suppressed {
			activeDeps++
		}
	}

	// FOCI stats
	fociPresentCount := 0
	packagesNoData := 0
	totalRepoFoci := 0
	for _, dep := range deps {
		if dep.Suppressed {
			continue
		}
		if len(dep.FOCI) == 0 {
			packagesNoData++
			continue
		}
		hasFoci := false
		if fociThreshold >= 0 {
			hasFoci = getFociChangeRatio(dep.FOCI)*100 > fociThreshold
		} else {
			hasFoci = getFociBool(dep.FOCI)
		}
		if hasFoci {
			fociPresentCount++
		}
		if repoFoci, ok := dep.FOCI["repository_foci"].([]any); ok {
			totalRepoFoci += len(repoFoci)
		}
	}
	passedCount := activeDeps - fociPresentCount - packagesNoData

	// Generate report
	fmt.Println("# Dependency FOCI Report")
	fmt.Printf("Generated: %s\n\n", time.Now().UTC().Format("2006-01-02 15:04:05 UTC"))
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

	fmt.Printf("Package manager dependencies found: %d\n", activeDeps)
	fmt.Println()

	// Step summary includes files scanned + all packages
	if fociSummary != nil && activeDeps > 0 {
		if len(fileOrder) > 0 {
			fmt.Fprintf(fociSummary, "<details>\n")
			fmt.Fprintf(fociSummary, "<summary><strong>📂 Files Scanned (%d files, %d packages)</strong></summary>\n\n", len(fileOrder), activeDeps)
			fmt.Fprintf(fociSummary, "<table>\n<tr><th>File</th><th>Packages</th></tr>\n")
			for _, f := range fileOrder {
				fmt.Fprintf(fociSummary, "<tr><td><code>%s</code></td><td>%d</td></tr>\n", f, fileDepCount[f])
			}
			fmt.Fprintf(fociSummary, "</table>\n\n</details>\n\n")
		}

		// All packages by ecosystem
		byEcoSummary := make(map[string][]Dependency)
		var ecoOrderSummary []string
		seenEcoSummary := make(map[string]bool)
		for _, dep := range deps {
			if dep.Suppressed {
				continue
			}
			if !seenEcoSummary[dep.Ecosystem] {
				seenEcoSummary[dep.Ecosystem] = true
				ecoOrderSummary = append(ecoOrderSummary, dep.Ecosystem)
			}
			byEcoSummary[dep.Ecosystem] = append(byEcoSummary[dep.Ecosystem], dep)
		}
		fmt.Fprintf(fociSummary, "<details>\n")
		fmt.Fprintf(fociSummary, "<summary><strong>📦 All Packages Scanned (%d)</strong></summary>\n\n", activeDeps)
		for _, eco := range ecoOrderSummary {
			fmt.Fprintf(fociSummary, "<p><strong>%s</strong></p>\n<ul>\n", eco)
			for _, dep := range byEcoSummary[eco] {
				status := "—"
				if len(dep.FOCI) > 0 {
					hasFoci := false
					if fociThreshold >= 0 {
						hasFoci = getFociChangeRatio(dep.FOCI)*100 > fociThreshold
					} else {
						hasFoci = getFociBool(dep.FOCI)
					}
					if hasFoci {
						status = "⚠️"
					} else {
						status = "✅"
					}
				}
				fmt.Fprintf(fociSummary, "<li>%s <code>%s</code></li>\n", status, dep.Name)
			}
			fmt.Fprintf(fociSummary, "</ul>\n")
		}
		fmt.Fprintf(fociSummary, "</details>\n\n")
	}

	if activeDeps > 0 {
		fmt.Println("### FOCI Analysis")
		fmt.Println()
		fmt.Printf("Passed: %d\n", passedCount)
		fmt.Printf("FOCI detected: %d\n", fociPresentCount)
		if packagesNoData > 0 {
			fmt.Printf("No data available: %d\n", packagesNoData)
		}
		fmt.Printf("Total repository FOCI locations: %d\n", totalRepoFoci)
		fmt.Println()

		if fociSummary != nil {
			fmt.Fprintf(fociSummary, "**Results:** %d passed · %d FOCI detected", passedCount, fociPresentCount)
			if packagesNoData > 0 {
				fmt.Fprintf(fociSummary, " · %d no data available", packagesNoData)
			}
			fmt.Fprintf(fociSummary, "\n\n")
		}

		baseURL := strings.TrimSuffix(depsDiverAPIURL, "/api")

		// Per-package foci details
		for _, dep := range deps {
			if dep.Suppressed || len(dep.FOCI) == 0 {
				continue
			}
			changeRatio := getFociChangeRatio(dep.FOCI)
			hasFoci := false
			if fociThreshold >= 0 {
				hasFoci = changeRatio*100 > fociThreshold
			} else {
				hasFoci = getFociBool(dep.FOCI)
			}
			repoFoci := getFociRepositoryFoci(dep.FOCI)
			if !hasFoci && len(repoFoci) == 0 {
				continue
			}
			if !hasFoci {
				continue
			}

			encodedPackage := url.QueryEscape(dep.Name)
			reportURL := fmt.Sprintf("%s/analyze/%s?ecosystem=%s#overview", baseURL, encodedPackage, dep.Ecosystem)
			owner := getFociString(dep.FOCI, "owner")
			repoName := getFociString(dep.FOCI, "name")
			fociStats := getFociStats(dep.FOCI)

			fmt.Printf("#### `%s` (%s)\n\n", dep.Name, dep.Ecosystem)
			fmt.Printf("**🔗 [View Full Report on Hunted Labs](%s)**\n\n", reportURL)
			if owner != "" && repoName != "" {
				fmt.Printf("**Repository:** `%s/%s`\n", owner, repoName)
			}
			fmt.Printf("**Total Foreign Contribution:** %s\n\n", formatPct(changeRatio))

			if len(fociStats) > 0 {
				fmt.Println("**Countries of Concern:**")
				for _, stat := range fociStats {
					if stat.FociPresent && stat.CountryName != "" {
						fmt.Printf("- %s — %s\n", stat.CountryName, formatPct(stat.ChangeRatio))
					}
				}
				fmt.Println()
			}

			if len(repoFoci) > 0 {
				fmt.Printf("**Repository FOCI (%d):**\n", len(repoFoci))
				for _, loc := range repoFoci {
					if loc["CountryName"] != "" {
						line := loc["CountryName"]
						if loc["OrganizationName"] != "" {
							line += fmt.Sprintf(" — %s", loc["OrganizationName"])
						}
						if loc["Reason"] != "" {
							line += fmt.Sprintf(" _(%s)_", loc["Reason"])
						}
						fmt.Printf("- %s\n", line)
					}
				}
				fmt.Println()
			}
			fmt.Println()

			if fociSummary != nil && getFociBool(dep.FOCI) {
				reportURLHTML := fmt.Sprintf("%s/analyze/%s?ecosystem=%s#overview", baseURL, url.QueryEscape(dep.Name), dep.Ecosystem)
				fmt.Fprintf(fociSummary, "<details>\n")
				fmt.Fprintf(fociSummary, "<summary><strong><code>%s</code></strong> (%s)", dep.Name, dep.Ecosystem)
				if owner != "" && repoName != "" {
					fmt.Fprintf(fociSummary, " — <code>%s/%s</code>", owner, repoName)
				}
				fmt.Fprintf(fociSummary, " — %s foreign contribution</summary>\n\n", formatPct(changeRatio))
				fmt.Fprintf(fociSummary, "<p>🔗 <a href=\"%s\"><strong>View Full Report on Hunted Labs</strong></a></p>\n\n", reportURLHTML)

				if len(fociStats) > 0 {
					fmt.Fprintf(fociSummary, "<table>\n<tr><th>Country</th><th>Contribution</th><th>Risk</th></tr>\n")
					for _, stat := range fociStats {
						if stat.FociPresent && stat.CountryName != "" {
							fmt.Fprintf(fociSummary, "<tr><td>%s</td><td>%s</td><td>⚠️ FOCI</td></tr>\n", stat.CountryName, formatPct(stat.ChangeRatio))
						}
					}
					fmt.Fprintf(fociSummary, "</table>\n\n")
				}

				if len(repoFoci) > 0 {
					fmt.Fprintf(fociSummary, "<p><strong>Repository FOCI (%d):</strong></p>\n<ul>\n", len(repoFoci))
					for _, loc := range repoFoci {
						if loc["CountryName"] != "" {
							line := fmt.Sprintf("<strong>%s</strong>", loc["CountryName"])
							if loc["OrganizationName"] != "" {
								line += fmt.Sprintf(" — %s", loc["OrganizationName"])
							}
							if loc["Reason"] != "" {
								line += fmt.Sprintf(" <em>(%s)</em>", loc["Reason"])
							}
							fmt.Fprintf(fociSummary, "<li>%s</li>\n", line)
						}
					}
					fmt.Fprintf(fociSummary, "</ul>\n\n")
				}
				fmt.Fprintf(fociSummary, "</details>\n\n")
			}
		}
	}

	// dependencies grouped by ecosystem
	byEco := make(map[string][]Dependency)
	for _, dep := range deps {
		if !dep.Suppressed {
			byEco[dep.Ecosystem] = append(byEco[dep.Ecosystem], dep)
		}
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
			if len(dep.FOCI) == 0 {
				fmt.Printf("- `%s` (no data available)\n", dep.Name)
				continue
			}
			hasFoci := false
			if fociThreshold >= 0 {
				hasFoci = getFociChangeRatio(dep.FOCI)*100 > fociThreshold
			} else {
				hasFoci = getFociBool(dep.FOCI)
			}
			if hasFoci {
				fmt.Printf("- `%s` ⚠️ FOCI detected (%s)\n", dep.Name, formatPct(getFociChangeRatio(dep.FOCI)))
			} else {
				fmt.Printf("- `%s`\n", dep.Name)
			}
		}
		fmt.Println()
	}

	if fociThreshold >= 0 && activeDeps > 0 {
		fmt.Println("---")
		fmt.Println()
		fmt.Println("## FOCI Threshold Summary")
		fmt.Println()
		fmt.Printf("Threshold: %.0f%% change ratio\n", fociThreshold)
		fmt.Printf("Packages above threshold: %d\n", fociPresentCount)
	}
}

func getFociBool(foci map[string]any) bool {
	v, ok := foci["foci"]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

func getFociString(foci map[string]any, key string) string {
	if v, ok := foci[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getFociChangeRatio(foci map[string]any) float64 {
	var total float64
	for _, stat := range getFociStats(foci) {
		if stat.FociPresent {
			total += stat.ChangeRatio
		}
	}
	return total
}

func getFociStats(foci map[string]any) []FociStat {
	raw, ok := foci["foci_stats"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	var stats []FociStat
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		stat := FociStat{}
		if v, ok := m["change_ratio"].(float64); ok {
			stat.ChangeRatio = v
		}
		if v, ok := m["country_name"].(string); ok {
			stat.CountryName = v
		}
		if v, ok := m["foci_present"].(bool); ok {
			stat.FociPresent = v
		}
		stats = append(stats, stat)
	}
	return stats
}

func getFociRepositoryFoci(foci map[string]any) []RepoFociLoc {
	raw, ok := foci["repository_foci"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	var locs []RepoFociLoc
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		loc := make(RepoFociLoc)
		for _, key := range []string{"CountryName", "OrganizationName", "Reason"} {
			if v, ok := m[key].(string); ok {
				loc[key] = v
			} else {
				loc[key] = ""
			}
		}
		locs = append(locs, loc)
	}
	return locs
}

func formatPct(ratio float64) string {
	pct := ratio * 100
	if pct > 0 && pct < 0.1 {
		return "<0.1%"
	}
	return fmt.Sprintf("%.1f%%", pct)
}
