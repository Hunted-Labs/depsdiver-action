package main

import (
	"fmt"
	"go/parser"
	"go/token"
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

