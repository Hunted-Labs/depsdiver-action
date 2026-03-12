package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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

	if depsDiverToken != "" && len(pkgManagerDeps) > 0 {
		fmt.Fprintf(os.Stderr, "Querying DepsDiver API for %d packages...\n", len(pkgManagerDeps))
		fetchedUserProfiles := make(map[int]*UserProfile)

		// Bulk query in chunks of 20
		const chunkSize = 20
		for i := 0; i < len(pkgManagerDeps); i += chunkSize {
			end := i + chunkSize
			if end > len(pkgManagerDeps) {
				end = len(pkgManagerDeps)
			}
			chunk := pkgManagerDeps[i:end]

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

		// Get OpenSSF scorecards
		for _, dep := range pkgManagerDeps {
			key := dep.Ecosystem + ":" + dep.Name
			info, ok := pkgManagerResults[key]
			if !ok || info.Error != "" || info.RepositoryID == 0 {
				continue
			}
			scorecard, err := queryOpenSSFScorecard(apiClient, depsDiverAPIURL, depsDiverToken, info.RepositoryID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to fetch OpenSSF scorecard for %s: %v\n", dep.Name, err)
			} else if scorecard != nil {
				info.OpenSSFScorecard = scorecard
			}
			time.Sleep(50 * time.Millisecond)
		}

		// Fetch user profiles for all unique user IDs
		fmt.Fprintf(os.Stderr, "Fetching user profiles...\n")
		for _, info := range pkgManagerResults {
			if info.Error != "" || info.UserFoci == nil {
				continue
			}
			for _, userFoci := range info.UserFoci {
				userID := userFoci.UserID
				if userID <= 0 {
					continue
				}
				if _, exists := fetchedUserProfiles[userID]; exists {
					continue
				}
				profile, err := queryUserProfile(apiClient, depsDiverAPIURL, depsDiverToken, userID)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: Failed to fetch user profile for ID %d: %v\n", userID, err)
				} else if profile != nil {
					fetchedUserProfiles[userID] = profile
				}
				time.Sleep(50 * time.Millisecond)
			}
		}

		// Assign fetched profiles to package infos
		for _, info := range pkgManagerResults {
			if info.Error != "" || info.UserFoci == nil {
				continue
			}
			if info.UserProfiles == nil {
				info.UserProfiles = make(map[int]*UserProfile)
			}
			for _, userFoci := range info.UserFoci {
				if userFoci.UserID > 0 {
					if profile, exists := fetchedUserProfiles[userFoci.UserID]; exists {
						info.UserProfiles[userFoci.UserID] = profile
					}
				}
			}
		}
		fmt.Fprintf(os.Stderr, "Fetched %d user profiles\n", len(fetchedUserProfiles))
	}

	// Calculate FOCI statistics
	fociPresentCount := 0
	totalRepoFoci := 0
	totalContributors := 0
	packagesWithErrors := 0
	packagesWithScorecard := 0
	lowScorePackages := 0
	totalOpenSSFScore := 0.0

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

	tallyResult := func(result *PackageInfo) {
		if result.Error != "" {
			packagesWithErrors++
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
		userSet := make(map[int]bool)
		for _, uf := range result.UserFoci {
			if uf.UserID > 0 {
				userSet[uf.UserID] = true
			}
		}
		totalContributors += len(userSet)
		if result.OpenSSFScorecard != nil {
			packagesWithScorecard++
			totalOpenSSFScore += result.OpenSSFScorecard.OverallScore
			if result.OpenSSFScorecard.OverallScore < 5.0 {
				lowScorePackages++
			}
		}
	}

	for _, result := range pkgManagerResults {
		tallyResult(result)
	}

	// Generate report
	fmt.Println("# Dependency FOCI Report")
	fmt.Printf("Generated: %s\n\n", getCurrentTime())

	fmt.Println("## Summary")
	fmt.Println()
	fmt.Printf("Package manager dependencies found: %d\n", len(pkgManagerDeps))
	fmt.Println()

	if len(pkgManagerResults) > 0 {
		fmt.Println("### FOCI Analysis")
		fmt.Println()
		fmt.Printf("Packages with FOCI present: %d\n", fociPresentCount)
		fmt.Printf("Total repository FOCI locations: %d\n", totalRepoFoci)
		fmt.Printf("Total contributors with FOCI: %d\n", totalContributors)

		if packagesWithScorecard > 0 {
			avgScore := totalOpenSSFScore / float64(packagesWithScorecard)
			fmt.Printf("\n**OpenSSF Scorecard Summary:**\n")
			fmt.Printf("Packages with OpenSSF Scorecard: %d\n", packagesWithScorecard)
			fmt.Printf("Average OpenSSF Score: %.1f/10\n", avgScore)
			if lowScorePackages > 0 {
				fmt.Printf("WARNING: Packages with low security score (<5): %d\n", lowScorePackages)
			}
		}

		if packagesWithErrors > 0 {
			fmt.Printf("\nPackages with API errors: %d\n", packagesWithErrors)
		}
		fmt.Println()

		if fociSummary != nil {
			fmt.Fprintf(fociSummary, "## Detailed FOCI Analysis\n\n")
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
				hasFociData = result.FociPresent || len(result.RepositoryFoci) > 0 || len(result.UserFoci) > 0
			}
			if !hasFociData {
				continue
			}

			encodedPackage := url.QueryEscape(dep.Name)
			baseURL := strings.TrimSuffix(depsDiverAPIURL, "/api")
			reportURL := fmt.Sprintf("%s/analyze/%s?ecosystem=%s#overview", baseURL, encodedPackage, dep.Ecosystem)

			fmt.Printf("#### `%s` (%s)\n", dep.Name, dep.Ecosystem)
			fmt.Println()
			fmt.Printf("**🔗 [View Full Report on Hunted Labs](%s)**\n", reportURL)
			fmt.Println()
			if result.Owner != "" && result.Name != "" {
				fmt.Printf("**Repository:** `%s/%s`\n", result.Owner, result.Name)
			}
			if result.RepositoryID != 0 {
				fmt.Printf("**Repository ID:** %d\n", result.RepositoryID)
			}

			if result.FociPresent {
				fmt.Printf("**FOCI Status:** DETECTED\n")
			} else {
				fmt.Printf("**FOCI Status:** NOT DETECTED\n")
			}
			fmt.Printf("**FOCI Change Ratio:** %.1f%%\n", result.ChangeRatio*100)

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

			if len(result.UserFoci) > 0 {
				userFociMap := make(map[int][]GeocodedLocation)
				for _, loc := range result.UserFoci {
					userFociMap[loc.UserID] = append(userFociMap[loc.UserID], loc)
				}

				fmt.Printf("\n**Contributor FOCI Analysis** (%d contributors):\n", len(userFociMap))
				for userID, fociEntries := range userFociMap {
					countries := make(map[string]bool)
					for _, f := range fociEntries {
						if f.CountryName != "" {
							countries[f.CountryName] = true
						}
					}
					countryList := make([]string, 0, len(countries))
					for c := range countries {
						countryList = append(countryList, c)
					}
					sort.Strings(countryList)

					if profile, exists := result.UserProfiles[userID]; exists && profile != nil {
						username := ""
						if len(profile.Logins) > 0 {
							username = profile.Logins[0]
						}
						displayName := ""
						if len(profile.Names) > 0 {
							displayName = profile.Names[0]
						}

						if username != "" {
							fmt.Printf("\n  - **@%s**", username)
							if displayName != "" && displayName != username {
								fmt.Printf(" (%s)", displayName)
							}
							fmt.Printf("\n")
						} else {
							fmt.Printf("\n  - **User ID %d**\n", userID)
						}

						if len(countryList) > 0 {
							fmt.Printf("    - **Countries:** %s\n", strings.Join(countryList, ", "))
						}
						if len(profile.Emails) > 0 {
							emailsToShow := profile.Emails
							if len(emailsToShow) > 3 {
								emailsToShow = emailsToShow[:3]
							}
							fmt.Printf("    - **Emails:** %s", strings.Join(emailsToShow, ", "))
							if len(profile.Emails) > 3 {
								fmt.Printf(" (+%d more)", len(profile.Emails)-3)
							}
							fmt.Printf("\n")
						}
						if len(profile.Locations) > 0 {
							fmt.Printf("    - **Locations:** %s\n", strings.Join(profile.Locations, ", "))
						}
						if len(profile.GeocodedLocation) > 0 {
							for _, gl := range profile.GeocodedLocation {
								info := gl.CountryName
								if gl.Formatted != "" {
									info = gl.Formatted
								}
								if gl.Reason != "" {
									info += fmt.Sprintf(" _(Reason: %s)_", gl.Reason)
								}
								fmt.Printf("    - **Geocoded Location:** %s\n", info)
							}
						}
						if len(profile.Companies) > 0 {
							companyNames := make([]string, 0, len(profile.Companies))
							for _, c := range profile.Companies {
								if c.Name != "" {
									companyNames = append(companyNames, c.Name)
								}
							}
							if len(companyNames) > 0 {
								fmt.Printf("    - **Companies:** %s\n", strings.Join(companyNames, ", "))
							}
						}
						if username != "" {
							fmt.Printf("    - **GitHub Profile:** https://github.com/%s\n", username)
						}
					} else {
						if userID > 0 {
							fmt.Printf("\n  - **User ID %d**\n", userID)
						}
						if len(countryList) > 0 {
							fmt.Printf("    - **Countries:** %s\n", strings.Join(countryList, ", "))
						}
						for _, f := range fociEntries {
							if f.Reason != "" {
								fmt.Printf("    - **Reason:** %s\n", f.Reason)
								break
							}
						}
					}
				}
			}

			if result.OpenSSFScorecard != nil {
				sc := result.OpenSSFScorecard
				fmt.Printf("\n**OpenSSF Security Scorecard**\n")
				fmt.Printf("- **Overall Score:** %.1f/10\n", sc.OverallScore)
				if sc.Date != "" {
					fmt.Printf("- **Assessment Date:** %s\n", sc.Date)
				}
				if sc.ScorecardVersion != "" {
					fmt.Printf("- **Scorecard Version:** %s\n", sc.ScorecardVersion)
				}
				concerningChecks := []OpenSSFIndividualCheck{}
				for _, check := range sc.IndividualResults {
					if check.Score >= 0 && check.Score < 5 {
						concerningChecks = append(concerningChecks, check)
					}
				}
				if len(concerningChecks) > 0 {
					fmt.Printf("\n**Security Concerns Identified** (%d checks with low scores):\n", len(concerningChecks))
					for _, check := range concerningChecks {
						scoreStr := fmt.Sprintf("%d/10", check.Score)
						if check.Score == -1 {
							scoreStr = "N/A"
						}
						fmt.Printf("  - **%s** (Score: %s): %s\n", check.Name, scoreStr, check.Reason)
					}
				}
			}

			fmt.Println()

			// Write to FOCI summary file for GitHub Actions
			if fociSummary != nil && result.FociPresent {
				encodedPackageHTML := url.QueryEscape(dep.Name)
				baseURLHTML := strings.TrimSuffix(depsDiverAPIURL, "/api")
				reportURLHTML := fmt.Sprintf("%s/analyze/%s?ecosystem=%s#overview", baseURLHTML, encodedPackageHTML, dep.Ecosystem)

				fmt.Fprintf(fociSummary, "<details>\n")
				fmt.Fprintf(fociSummary, "<summary><strong>Package: <code>%s</code></strong> (%s)", dep.Name, dep.Ecosystem)
				if result.Owner != "" && result.Name != "" {
					fmt.Fprintf(fociSummary, " - <code>%s/%s</code>", result.Owner, result.Name)
				}
				fmt.Fprintf(fociSummary, "</summary>\n\n")
				fmt.Fprintf(fociSummary, "<p>🔗 <a href=\"%s\"><strong>View Full Report on Hunted Labs</strong></a></p>\n\n", reportURLHTML)

				if len(result.RepositoryFoci) > 0 {
					fmt.Fprintf(fociSummary, "<p><strong>Repository FOCI Locations:</strong> %d location(s)</p>\n", len(result.RepositoryFoci))
					fmt.Fprintf(fociSummary, "<details>\n")
					fmt.Fprintf(fociSummary, "<summary>View Repository Location Details</summary>\n")
					fmt.Fprintf(fociSummary, "<ul>\n")
					for _, loc := range result.RepositoryFoci {
						if loc.CountryName != "" {
							flag := ""
							if loc.ISO3166Alpha2 != "" {
								flag = fmt.Sprintf(" (%s)", loc.ISO3166Alpha2)
							}
							orgInfo := ""
							if loc.OrganizationName != "" {
								orgInfo = fmt.Sprintf(" - <em>%s</em>", loc.OrganizationName)
							}
							fmt.Fprintf(fociSummary, "<li><strong>%s</strong>%s%s</li>\n", loc.CountryName, flag, orgInfo)
						}
					}
					fmt.Fprintf(fociSummary, "</ul>\n")
					fmt.Fprintf(fociSummary, "</details>\n\n")
				}

				if len(result.UserFoci) > 0 {
					userFociMap := make(map[int][]GeocodedLocation)
					for _, loc := range result.UserFoci {
						userFociMap[loc.UserID] = append(userFociMap[loc.UserID], loc)
					}

					fmt.Fprintf(fociSummary, "<p><strong>Contributor FOCI:</strong> %d contributor(s)</p>\n", len(userFociMap))

					for userID, fociEntries := range userFociMap {
						countries := make(map[string]bool)
						for _, f := range fociEntries {
							if f.CountryName != "" {
								countries[f.CountryName] = true
							}
						}
						countryList := make([]string, 0, len(countries))
						for c := range countries {
							countryList = append(countryList, c)
						}
						sort.Strings(countryList)

						if profile, exists := result.UserProfiles[userID]; exists && profile != nil {
							username := ""
							if len(profile.Logins) > 0 {
								username = profile.Logins[0]
							}
							displayName := ""
							if len(profile.Names) > 0 {
								displayName = profile.Names[0]
							}

							if username != "" {
								fmt.Fprintf(fociSummary, "<details>\n")
								fmt.Fprintf(fociSummary, "<summary><strong>@%s</strong>", username)
								if displayName != "" && displayName != username {
									fmt.Fprintf(fociSummary, " (%s)", displayName)
								}
								fmt.Fprintf(fociSummary, " - %s</summary>\n", strings.Join(countryList, ", "))
								fmt.Fprintf(fociSummary, "<ul>\n")
								if len(countryList) > 0 {
									fmt.Fprintf(fociSummary, "<li><strong>Countries:</strong> %s</li>\n", strings.Join(countryList, ", "))
								}
								if len(profile.Emails) > 0 {
									fmt.Fprintf(fociSummary, "<li><strong>Email Addresses:</strong> %s</li>\n", strings.Join(profile.Emails, ", "))
								}
								if len(profile.Locations) > 0 {
									fmt.Fprintf(fociSummary, "<li><strong>Locations:</strong> %s</li>\n", strings.Join(profile.Locations, ", "))
								}
								if len(profile.GeocodedLocation) > 0 {
									for _, gl := range profile.GeocodedLocation {
										info := gl.CountryName
										if gl.Formatted != "" {
											info = gl.Formatted
										}
										if gl.Reason != "" {
											info += fmt.Sprintf(" <em>(Reason: %s)</em>", gl.Reason)
										}
										fmt.Fprintf(fociSummary, "<li><strong>Geocoded Location:</strong> %s</li>\n", info)
									}
								}
								if len(profile.Companies) > 0 {
									companyNames := make([]string, 0)
									for _, c := range profile.Companies {
										if c.Name != "" {
											companyNames = append(companyNames, c.Name)
										}
									}
									if len(companyNames) > 0 {
										fmt.Fprintf(fociSummary, "<li><strong>Company Affiliations:</strong> %s</li>\n", strings.Join(companyNames, ", "))
									}
								}
								fmt.Fprintf(fociSummary, "<li><strong>Profile:</strong> <a href=\"https://github.com/%s\">https://github.com/%s</a></li>\n", username, username)
								fmt.Fprintf(fociSummary, "</ul>\n")
								fmt.Fprintf(fociSummary, "</details>\n")
							} else {
								fmt.Fprintf(fociSummary, "<p><strong>User ID %d</strong> - %s</p>\n", userID, strings.Join(countryList, ", "))
							}
						} else {
							if userID > 0 {
								fmt.Fprintf(fociSummary, "<p><strong>User ID %d</strong> - %s</p>\n", userID, strings.Join(countryList, ", "))
							} else {
								fmt.Fprintf(fociSummary, "<p>%s</p>\n", strings.Join(countryList, ", "))
							}
						}
					}
					fmt.Fprintf(fociSummary, "\n")
				}

				if result.OpenSSFScorecard != nil {
					sc := result.OpenSSFScorecard
					fmt.Fprintf(fociSummary, "<p><strong>OpenSSF Security Score:</strong> %.1f/10</p>\n", sc.OverallScore)
					if len(sc.IndividualResults) > 0 {
						fmt.Fprintf(fociSummary, "<details>\n")
						fmt.Fprintf(fociSummary, "<summary><strong>View Security Assessment Details</strong> (%d checks)</summary>\n\n", len(sc.IndividualResults))
						fmt.Fprintf(fociSummary, "<table>\n")
						fmt.Fprintf(fociSummary, "<tr><th>Check</th><th>Score</th><th>Assessment</th></tr>\n")
						for _, check := range sc.IndividualResults {
							scoreStr := fmt.Sprintf("%d/10", check.Score)
							if check.Score == -1 {
								scoreStr = "N/A"
							}
							rowClass := ""
							if check.Score >= 0 && check.Score < 5 {
								rowClass = " style=\"background-color: #ffebee;\""
							} else if check.Score >= 5 && check.Score < 7 {
								rowClass = " style=\"background-color: #fff3e0;\""
							}
							fmt.Fprintf(fociSummary, "<tr%s><td><strong>%s</strong></td><td>%s</td><td>%s</td></tr>\n",
								rowClass, check.Name, scoreStr, check.Reason)
						}
						fmt.Fprintf(fociSummary, "</table>\n")
						fmt.Fprintf(fociSummary, "</details>\n")
					}
				}

				fmt.Fprintf(fociSummary, "</details>\n\n")
			}
		}

		// Error section
		if packagesWithErrors > 0 {
			fmt.Println("#### API Query Errors")
			fmt.Println()
			for _, dep := range pkgManagerDeps {
				key := dep.Ecosystem + ":" + dep.Name
				if result, exists := pkgManagerResults[key]; exists && result.Error != "" {
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
					if result, exists := pkgManagerResults[key]; exists && result.Error != "" {
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
					fmt.Printf("- `%s` (API error: %s)\n", dep.Name, result.Error)
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
	ImportPath       string
	Ecosystem        string
	RepositoryID     int64
	Owner            string
	Name             string
	Package          string
	FociPresent      bool
	ChangeRatio      float64 // 0.0-1.0: fraction of changes from FOCI-linked contributors
	RepositoryFoci   []GeocodedPkgLocation
	UserFoci         []GeocodedLocation
	UserProfiles     map[int]*UserProfile // User ID -> Profile
	OpenSSFScorecard *OpenSSFScorecard    // OpenSSF scorecard data
	Error            string
}

// user geocoded location data
type GeocodedLocation struct {
	UserID                 int    `json:"UserID"`
	Login                  string `json:"Login"`
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

// user profile data from /api/user/id/{id}
type UserProfile struct {
	ID               int                    `json:"ID"`
	URL              string                 `json:"URL"`
	Logins           []string               `json:"Logins"`
	Names            []string               `json:"Names"`
	Emails           []string               `json:"Emails"`
	Avatars          []string               `json:"Avatars"`
	Bios             []string               `json:"Bios"`
	Companies        []Company              `json:"Companies"`
	Twitter          []string               `json:"Twitter"`
	Websites         []string               `json:"Websites"`
	Repositories     []Repository           `json:"Repositories"`
	Locations        []string               `json:"Locations"`
	GeocodedLocation []GeocodedUserLocation `json:"GeocodedLocation"`
}

// company association
type Company struct {
	Name string `json:"Name"`
	URL  string `json:"URL"`
}

// repository reference
type Repository struct {
	ID        int    `json:"ID"`
	URL       string `json:"URL"`
	Ecosystem string `json:"Ecosystem"`
}

// geocoded location for a user profile
type GeocodedUserLocation struct {
	Formatted      string `json:"Formatted"`
	CountryName    string `json:"CountryName"`
	ISO3166Alpha2  string `json:"ISO3166Alpha2"`
	Reason         string `json:"Reason"`
	Timezone       string `json:"Timezone"`
	TimezoneOffset string `json:"TimezoneOffset"`
}

// OpenSSF scorecard data
type OpenSSFScorecard struct {
	Date              string                   `json:"Date"`
	ScorecardVersion  string                   `json:"ScorecardVersion"`
	OverallScore      float64                  `json:"OverallScore"`
	IndividualResults []OpenSSFIndividualCheck `json:"IndividualResults"`
}

// individual check in the scorecard
type OpenSSFIndividualCheck struct {
	Name             string   `json:"Name"`
	ShortDescription string   `json:"ShortDescription"`
	URL              string   `json:"URL"`
	Score            int      `json:"Score"`
	Reason           string   `json:"Reason"`
	Details          []string `json:"Details"`
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
		UserFoci  []GeocodedLocation    `json:"user_foci"`
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
		for _, stat := range pkgInfo.FociStats {
			if stat.FociPresent {
				fociChangeRatio += stat.ChangeRatio
			}
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
			UserFoci:       pkgInfo.UserFoci,
			UserProfiles:   make(map[int]*UserProfile),
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

	// Parse the JSON response - GetPackagesFociResponse is a map[string]*PackageFoci
	var apiResponse map[string]*struct {
		RepoID    int64                 `json:"repo_id"`
		Owner     string                `json:"owner"`
		Name      string                `json:"name"`
		Package   string                `json:"package"`
		Foci      bool                  `json:"foci"`
		RepoFoci  []GeocodedPkgLocation `json:"repository_foci"`
		UserFoci  []GeocodedLocation    `json:"user_foci"`
		FociStats []struct {
			ChangeRatio float64 `json:"change_ratio"`
			CountryName *string `json:"country_name"`
			FociPresent bool    `json:"foci_present"`
		} `json:"foci_stats"`
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

	// Sum change_ratio for all foci_present entries to get total FOCI change ratio
	var fociChangeRatio float64
	for _, stat := range pkgInfo.FociStats {
		if stat.FociPresent {
			fociChangeRatio += stat.ChangeRatio
		}
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
		UserFoci:       pkgInfo.UserFoci,
		UserProfiles:   make(map[int]*UserProfile),
	}, nil
}

// queryUserProfile fetches user profile from /api/user/id/{userId}
func queryUserProfile(client *http.Client, apiURL, token string, userID int) (*UserProfile, error) {
	apiEndpoint := fmt.Sprintf("%s/user/id/%d", strings.TrimSuffix(apiURL, "/"), userID)

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

	var profile UserProfile
	if err := json.Unmarshal(body, &profile); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &profile, nil
}

// queryOpenSSFScorecard fetches OpenSSF scorecard from /api/repository/{repoId}/ossf_scorecards
func queryOpenSSFScorecard(client *http.Client, apiURL, token string, repoID int64) (*OpenSSFScorecard, error) {
	apiEndpoint := fmt.Sprintf("%s/repository/%d/ossf_scorecards", strings.TrimSuffix(apiURL, "/"), repoID)

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

	// API returns an array of scorecards, we want the first (most recent)
	var scorecards []OpenSSFScorecard
	if err := json.Unmarshal(body, &scorecards); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(scorecards) == 0 {
		return nil, nil
	}

	return &scorecards[0], nil
}
