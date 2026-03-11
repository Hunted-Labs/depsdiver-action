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
	allImports := make(map[string]map[string]bool) // file -> imports
	uniqueImports := make(map[string]bool)
	githubImports := make(map[string]bool) // All third-party packages (GitHub, golang.org, etc.)
	standardLibImports := make(map[string]bool)
	githubPackageFiles := make(map[string][]string) // package -> []files that use it (all third-party)

	// Get DepsDiver API configuration from environment
	depsDiverToken := os.Getenv("DEPSDIVER_TOKEN")
	depsDiverAPIURL := os.Getenv("DEPSDIVER_API_URL")

	// Get FOCI threshold (-1 means disabled; otherwise 0-100 percent)
	// Packages with change_ratio above this threshold are flagged in the report.
	fociThreshold := -1.0
	if thresholdStr := os.Getenv("FOCI_THRESHOLD"); thresholdStr != "" {
		if t, err := strconv.ParseFloat(thresholdStr, 64); err == nil && t >= 0 && t <= 100 {
			fociThreshold = t
		}
	}
	if depsDiverAPIURL == "" {
		depsDiverAPIURL = "https://api.example.com" // default, should be overridden
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
			} else if isThirdPartyPackage(importPath) {
				// All third-party packages (GitHub and others)
				githubImports[importPath] = true
				// Track which files use this package
				githubPackageFiles[importPath] = append(githubPackageFiles[importPath], relPath)
			} else {
				// Fallback for any other imports
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

	// Query DepsDiver API for each third-party package if token is provided
	if depsDiverToken != "" && len(githubImports) > 0 {
		fmt.Fprintf(os.Stderr, "Querying DepsDiver API for %d third-party packages...\n", len(githubImports))
		client := &http.Client{
			Timeout: 30 * time.Second,
		}

		// Track unique user IDs to avoid duplicate fetches
		fetchedUserProfiles := make(map[int]*UserProfile)

		for importPath := range githubImports {
			info, err := queryDepsDiverAPI(client, depsDiverAPIURL, depsDiverToken, importPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to query API for %s: %v\n", importPath, err)
				githubImportResults[importPath] = &PackageInfo{
					ImportPath: importPath,
					Error:      err.Error(),
				}
			} else {
				githubImportResults[importPath] = info

				// Fetch OpenSSF scorecard for the repository
				if info.RepositoryID > 0 {
					scorecard, err := queryOpenSSFScorecard(client, depsDiverAPIURL, depsDiverToken, info.RepositoryID)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Warning: Failed to fetch OpenSSF scorecard for %s: %v\n", importPath, err)
					} else if scorecard != nil {
						info.OpenSSFScorecard = scorecard
					}
					time.Sleep(50 * time.Millisecond) // Small delay
				}
			}
			// Small delay to avoid rate limiting
			time.Sleep(100 * time.Millisecond)
		}

		// Second pass: fetch user profiles for all unique user IDs across all packages
		fmt.Fprintf(os.Stderr, "Fetching user profiles...\n")
		for _, info := range githubImportResults {
			if info.Error != "" || info.UserFoci == nil {
				continue
			}
			for _, userFoci := range info.UserFoci {
				userID := userFoci.UserID
				if userID <= 0 {
					continue
				}
				// Check if already fetched
				if _, exists := fetchedUserProfiles[userID]; exists {
					continue
				}

				profile, err := queryUserProfile(client, depsDiverAPIURL, depsDiverToken, userID)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: Failed to fetch user profile for ID %d: %v\n", userID, err)
				} else if profile != nil {
					fetchedUserProfiles[userID] = profile
				}
				time.Sleep(50 * time.Millisecond) // Small delay
			}
		}

		// Assign fetched profiles to package infos
		for _, info := range githubImportResults {
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

	// Generate report
	fmt.Println("# Go Imports Report")
	fmt.Println("(Standard library and recognized third-party packages filtered out)")
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

	for _, result := range githubImportResults {
		if result.Error != "" {
			packagesWithErrors++
		} else {
			// When threshold is set, count packages exceeding the change_ratio threshold.
			// When unset, fall back to the boolean FociPresent flag.
			if fociThreshold >= 0 {
				if result.ChangeRatio*100 > fociThreshold {
					fociPresentCount++
				}
			} else if result.FociPresent {
				fociPresentCount++
			}
			totalRepoFoci += len(result.RepositoryFoci)
			totalUserFoci += len(result.UserFoci)

			// Count unique contributors
			userSet := make(map[int]bool)
			for _, uf := range result.UserFoci {
				if uf.UserID > 0 {
					userSet[uf.UserID] = true
				}
			}
			totalContributors += len(userSet)

			// OpenSSF statistics
			if result.OpenSSFScorecard != nil {
				packagesWithScorecard++
				totalOpenSSFScore += result.OpenSSFScorecard.OverallScore
				if result.OpenSSFScorecard.OverallScore < 5.0 {
					lowScorePackages++
				}
			}
		}
	}

	// Summary
	fmt.Println("---")
	fmt.Println()
	fmt.Println("## Summary")
	fmt.Println()
	fmt.Printf("Total other imports (excluding stdlib and recognized third-party): %d\n", totalImports)
	fmt.Printf("Unique other imports: %d\n", len(uniqueImports))
	fmt.Printf("Third-party packages found: %d\n", len(githubImports))
	fmt.Printf("Standard library packages found: %d\n", len(standardLibImports))
	fmt.Println()

	// FOCI Summary with detailed information
	if len(githubImportResults) > 0 {
		fmt.Println("### FOCI Analysis")
		fmt.Println()
		fmt.Printf("Packages with FOCI present: %d\n", fociPresentCount)
		fmt.Printf("Total repository FOCI locations: %d\n", totalRepoFoci)
		fmt.Printf("Total contributors with FOCI: %d\n", totalContributors)

		// OpenSSF Scorecard summary
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

		// Detailed FOCI information by package
		githubList := make([]string, 0, len(githubImports))
		for imp := range githubImports {
			githubList = append(githubList, imp)
		}
		sort.Strings(githubList)

		// Write FOCI summary for GitHub Actions
		if fociSummary != nil {
			fmt.Fprintf(fociSummary, "## Detailed FOCI Analysis\n\n")
		}

		for _, imp := range githubList {
			if result, exists := githubImportResults[imp]; exists && result.Error == "" {
				var hasFociData bool
				if fociThreshold >= 0 {
					// Only flag packages where change_ratio exceeds threshold
					hasFociData = result.ChangeRatio*100 > fociThreshold
				} else {
					hasFociData = result.FociPresent || len(result.RepositoryFoci) > 0 || len(result.UserFoci) > 0
				}
				if hasFociData {
					// Get files that use this package
					files := githubPackageFiles[imp]
					sort.Strings(files)

					// Generate report URL
					encodedPackage := url.QueryEscape(imp)
					baseURL := strings.TrimSuffix(depsDiverAPIURL, "/api")
					reportURL := fmt.Sprintf("%s/analyze/%s?ecosystem=go#overview", baseURL, encodedPackage)

					fmt.Printf("#### `%s`\n", imp)
					fmt.Println()
					fmt.Printf("**🔗 [View Full Report on Hunted Labs](%s)**\n", reportURL)
					fmt.Println()
					if result.Owner != "" && result.Name != "" {
						fmt.Printf("**Repository:** `%s/%s`\n", result.Owner, result.Name)
					}
					if result.RepositoryID != 0 {
						fmt.Printf("**Repository ID:** %d\n", result.RepositoryID)
					}

					// FOCI Status
					if result.FociPresent {
						fmt.Printf("**FOCI Status:** DETECTED\n")
					} else {
						fmt.Printf("**FOCI Status:** NOT DETECTED\n")
					}
					fmt.Printf("**FOCI Change Ratio:** %.1f%%\n", result.ChangeRatio*100)

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

					// User FOCI - Enhanced with user profile information
					if len(result.UserFoci) > 0 {
						// Group by user ID
						userFociMap := make(map[int][]GeocodedLocation)
						for _, loc := range result.UserFoci {
							userID := loc.UserID
							userFociMap[userID] = append(userFociMap[userID], loc)
						}

						fmt.Printf("\n**Contributor FOCI Analysis** (%d contributors):\n", len(userFociMap))
						for userID, fociEntries := range userFociMap {
							// Get unique countries for this user
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

							// Get user profile if available
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

								// Show countries
								if len(countryList) > 0 {
									fmt.Printf("    - **Countries:** %s\n", strings.Join(countryList, ", "))
								}

								// Show emails (first few)
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

								// Show locations
								if len(profile.Locations) > 0 {
									fmt.Printf("    - **Locations:** %s\n", strings.Join(profile.Locations, ", "))
								}

								// Show geocoded locations with reasons
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

								// Show companies
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

								// GitHub profile link
								if username != "" {
									fmt.Printf("    - **GitHub Profile:** https://github.com/%s\n", username)
								}
							} else {
								// No profile available, show basic info
								if userID > 0 {
									fmt.Printf("\n  - **User ID %d**\n", userID)
								}
								if len(countryList) > 0 {
									fmt.Printf("    - **Countries:** %s\n", strings.Join(countryList, ", "))
								}
								// Show reasons from FOCI entries
								for _, f := range fociEntries {
									if f.Reason != "" {
										fmt.Printf("    - **Reason:** %s\n", f.Reason)
										break // Just show first reason
									}
								}
							}
						}
					}
					// OpenSSF Scorecard
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

						// Show individual checks with concerning scores (< 5)
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
						// Generate report URL for HTML summary
						encodedPackageHTML := url.QueryEscape(imp)
						baseURLHTML := strings.TrimSuffix(depsDiverAPIURL, "/api")
						reportURLHTML := fmt.Sprintf("%s/analyze/%s?ecosystem=go#overview", baseURLHTML, encodedPackageHTML)

						// Create expandable section for each package
						fmt.Fprintf(fociSummary, "<details>\n")
						fmt.Fprintf(fociSummary, "<summary><strong>Package: <code>%s</code></strong>", imp)

						if result.Owner != "" && result.Name != "" {
							fmt.Fprintf(fociSummary, " - <code>%s/%s</code>", result.Owner, result.Name)
						}
						fmt.Fprintf(fociSummary, "</summary>\n\n")

						// Add link to full report
						fmt.Fprintf(fociSummary, "<p>🔗 <a href=\"%s\"><strong>View Full Report on Hunted Labs</strong></a></p>\n\n", reportURLHTML)

						// Files using this package
						if len(files) > 0 {
							fmt.Fprintf(fociSummary, "<p><strong>Files Using Package:</strong> %d file(s)</p>\n", len(files))
							fmt.Fprintf(fociSummary, "<details>\n")
							fmt.Fprintf(fociSummary, "<summary>View File List</summary>\n")
							fmt.Fprintf(fociSummary, "<ul>\n")
							for _, file := range files {
								fmt.Fprintf(fociSummary, "<li><code>%s</code></li>\n", file)
							}
							fmt.Fprintf(fociSummary, "</ul>\n")
							fmt.Fprintf(fociSummary, "</details>\n\n")
						}

						// Repository FOCI locations
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

						// User FOCI locations - Enhanced with user profiles
						if len(result.UserFoci) > 0 {
							// Group by user ID
							userFociMap := make(map[int][]GeocodedLocation)
							for _, loc := range result.UserFoci {
								userFociMap[loc.UserID] = append(userFociMap[loc.UserID], loc)
							}

							fmt.Fprintf(fociSummary, "<p><strong>Contributor FOCI:</strong> %d contributor(s)</p>\n", len(userFociMap))

							for userID, fociEntries := range userFociMap {
								// Get unique countries
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

								// Get user profile if available
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

										// Countries
										if len(countryList) > 0 {
											fmt.Fprintf(fociSummary, "<li><strong>Countries:</strong> %s</li>\n", strings.Join(countryList, ", "))
										}

										// Emails
										if len(profile.Emails) > 0 {
											fmt.Fprintf(fociSummary, "<li><strong>Email Addresses:</strong> %s</li>\n", strings.Join(profile.Emails, ", "))
										}

										// Locations
										if len(profile.Locations) > 0 {
											fmt.Fprintf(fociSummary, "<li><strong>Locations:</strong> %s</li>\n", strings.Join(profile.Locations, ", "))
										}

										// Geocoded locations
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

										// Companies
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

										// GitHub link
										fmt.Fprintf(fociSummary, "<li><strong>Profile:</strong> <a href=\"https://github.com/%s\">https://github.com/%s</a></li>\n", username, username)

										fmt.Fprintf(fociSummary, "</ul>\n")
										fmt.Fprintf(fociSummary, "</details>\n")
									} else {
										fmt.Fprintf(fociSummary, "<p><strong>User ID %d</strong> - %s</p>\n", userID, strings.Join(countryList, ", "))
									}
								} else {
									// No profile available
									if userID > 0 {
										fmt.Fprintf(fociSummary, "<p><strong>User ID %d</strong> - %s</p>\n", userID, strings.Join(countryList, ", "))
									} else {
										fmt.Fprintf(fociSummary, "<p>%s</p>\n", strings.Join(countryList, ", "))
									}
								}
							}
							fmt.Fprintf(fociSummary, "\n")
						}

						// OpenSSF Scorecard section
						if result.OpenSSFScorecard != nil {
							sc := result.OpenSSFScorecard
							fmt.Fprintf(fociSummary, "<p><strong>OpenSSF Security Score:</strong> %.1f/10</p>\n", sc.OverallScore)

							// Show all checks in a collapsible section with table
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

									// Color code based on score
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
			}
		}

		// Add error section to FOCI summary
		if fociSummary != nil && packagesWithErrors > 0 {
			fmt.Fprintf(fociSummary, "### API Query Errors\n\n")
			fmt.Fprintf(fociSummary, "<details>\n")
			fmt.Fprintf(fociSummary, "<summary><strong>View Package Query Errors</strong> (%d packages)</summary>\n\n", packagesWithErrors)
			fmt.Fprintf(fociSummary, "<table>\n")
			fmt.Fprintf(fociSummary, "<tr><th>Package</th><th>Error Message</th></tr>\n")
			for _, imp := range githubList {
				if result, exists := githubImportResults[imp]; exists && result.Error != "" {
					fmt.Fprintf(fociSummary, "<tr><td><code>%s</code></td><td>%s</td></tr>\n", imp, result.Error)
				}
			}
			fmt.Fprintf(fociSummary, "</table>\n\n")
			fmt.Fprintf(fociSummary, "</details>\n\n")
		}

		// List packages with errors
		if packagesWithErrors > 0 {
			fmt.Println("#### API Query Errors")
			fmt.Println()
			for _, imp := range githubList {
				if result, exists := githubImportResults[imp]; exists && result.Error != "" {
					fmt.Printf("- `%s`: %s\n", imp, result.Error)
				}
			}
			fmt.Println()
		}
	}

	// Third-party packages section (just list, no FOCI details)
	if len(githubImports) > 0 {
		fmt.Println("### Third-Party Packages")
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

	// Other imports (non-stdlib, non-recognized third-party)
	if len(uniqueImports) > 0 {
		fmt.Println("### Other Imports (excluding stdlib and recognized third-party)")
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

	// FOCI threshold summary
	if fociThreshold >= 0 && len(githubImportResults) > 0 {
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

// isThirdPartyPackage checks if an import path is from a third-party source.
// This includes github.com, golang.org, go.opentelemetry.io, google.golang.org, etc.
// Any package with a dot in the first segment is considered third-party.
func isThirdPartyPackage(importPath string) bool {
	if importPath == "" {
		return false
	}

	// Get the first segment of the path
	firstSegment := strings.Split(importPath, "/")[0]

	// Third-party packages have dots in the first segment (domain names)
	// Examples: "github.com", "golang.org", "go.opentelemetry.io", "google.golang.org"
	return strings.Contains(firstSegment, ".")
}

// PackageInfo represents the information returned from the DepsDiver API
type PackageInfo struct {
	ImportPath       string
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

// GeocodedLocation represents user geocoded location data
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

// GeocodedPkgLocation represents geocoded location data
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

// UserProfile represents user profile data from /api/user/id/{id}
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

// Company represents a company association
type Company struct {
	Name string `json:"Name"`
	URL  string `json:"URL"`
}

// Repository represents a repository reference
type Repository struct {
	ID        int    `json:"ID"`
	URL       string `json:"URL"`
	Ecosystem string `json:"Ecosystem"`
}

// GeocodedUserLocation represents geocoded location for a user profile
type GeocodedUserLocation struct {
	Formatted      string `json:"Formatted"`
	CountryName    string `json:"CountryName"`
	ISO3166Alpha2  string `json:"ISO3166Alpha2"`
	Reason         string `json:"Reason"`
	Timezone       string `json:"Timezone"`
	TimezoneOffset string `json:"TimezoneOffset"`
}

// OpenSSFScorecard represents the OpenSSF scorecard data
type OpenSSFScorecard struct {
	Date              string                   `json:"Date"`
	ScorecardVersion  string                   `json:"ScorecardVersion"`
	OverallScore      float64                  `json:"OverallScore"`
	IndividualResults []OpenSSFIndividualCheck `json:"IndividualResults"`
}

// OpenSSFIndividualCheck represents an individual check in the scorecard
type OpenSSFIndividualCheck struct {
	Name             string   `json:"Name"`
	ShortDescription string   `json:"ShortDescription"`
	URL              string   `json:"URL"`
	Score            int      `json:"Score"`
	Reason           string   `json:"Reason"`
	Details          []string `json:"Details"`
}

// queryDepsDiverAPI queries the DepsDiver API for package information
func queryDepsDiverAPI(client *http.Client, apiURL, token, importPath string) (*PackageInfo, error) {
	// For GitHub packages, use "go" as ecosystem and the full import path as package name
	// URL encode the package name
	encodedPackage := url.QueryEscape(importPath)
	// Use the /foci/present endpoint
	apiEndpoint := fmt.Sprintf("%s/foci/present/go/%s", strings.TrimSuffix(apiURL, "/"), encodedPackage)

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

	// DEBUG: print raw response to see field names
	fmt.Fprintf(os.Stderr, "DEBUG API response for %s: %s\n", importPath, string(body))

	// Parse the JSON response - GetPackagesFociResponse is a map[string]*PackageFoci
	var apiResponse map[string]*struct {
		RepoID      int64                 `json:"repo_id"`
		Owner       string                `json:"owner"`
		Name        string                `json:"name"`
		Package     string                `json:"package"`
		Foci        bool                  `json:"foci"`
		ChangeRatio float64               `json:"change_ratio"`
		RepoFoci    []GeocodedPkgLocation `json:"repository_foci"`
		UserFoci    []GeocodedLocation    `json:"user_foci"`
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
		ChangeRatio:    pkgInfo.ChangeRatio,
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
