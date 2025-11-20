package main

import (
	"fmt"
	"go/ast"
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
			imports[importPath] = true
			uniqueImports[importPath] = true
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
	fmt.Printf("Total import statements found: %d\n", totalImports)
	fmt.Printf("Unique imports: %d\n", len(uniqueImports))
	fmt.Println()
	fmt.Println("### All Unique Imports")
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

func getCurrentTime() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05 UTC")
}

