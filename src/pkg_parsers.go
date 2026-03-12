package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type PackageManagerDep struct {
	Name       string
	Version    string
	Ecosystem  string // "go", "npm", "pypi", "cargo", "rubygems", "maven", "nuget"
	SourceFile string // relative path to the file
}

func isPackageManagerFile(name string) bool {
	switch name {
	case "go.mod", "package.json", "requirements.txt", "pyproject.toml",
		"Pipfile", "Cargo.toml", "Gemfile", "pom.xml",
		"build.gradle", "build.gradle.kts", "libs.versions.toml":
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".csproj" || ext == ".vbproj" || ext == ".fsproj"
}

// walks rootDir and parses all package manager files
func scanPackageManagerFiles(rootDir string) ([]PackageManagerDep, error) {
	var deps []PackageManagerDep

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case "vendor", ".git", "node_modules", "target", "build", "dist", ".idea", "__pycache__":
				return filepath.SkipDir
			}
			return nil
		}
		if !isPackageManagerFile(info.Name()) {
			return nil
		}

		relPath, _ := filepath.Rel(rootDir, path)
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		var parsed []PackageManagerDep
		switch info.Name() {
		case "go.mod":
			parsed = parseGoModFile(string(content), relPath)
		case "package.json":
			parsed = parsePackageJSONFile(string(content), relPath)
		case "requirements.txt":
			parsed = parseRequirementsTxtFile(string(content), relPath)
		case "pyproject.toml":
			parsed = parsePyprojectTomlFile(string(content), relPath)
		case "Pipfile":
			parsed = parsePipfileFile(string(content), relPath)
		case "Cargo.toml":
			parsed = parseCargoTomlFile(string(content), relPath)
		case "Gemfile":
			parsed = parseGemfileFile(string(content), relPath)
		case "pom.xml":
			parsed = parsePomXmlFile(string(content), relPath)
		case "build.gradle", "build.gradle.kts":
			parsed = parseGradleFile(string(content), relPath)
		case "libs.versions.toml":
			parsed = parseVersionCatalogFile(string(content), relPath)
		default:
			ext := strings.ToLower(filepath.Ext(info.Name()))
			if ext == ".csproj" || ext == ".vbproj" || ext == ".fsproj" {
				parsed = parseCsProjFile(string(content), relPath)
			}
		}
		deps = append(deps, parsed...)
		return nil
	})

	return deps, err
}

func parseGoModFile(content, relPath string) []PackageManagerDep {
	var deps []PackageManagerDep
	lines := strings.Split(content, "\n")
	inRequireBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if trimmed == "require (" || trimmed == "require(" {
			inRequireBlock = true
			continue
		}
		if trimmed == ")" && inRequireBlock {
			inRequireBlock = false
			continue
		}

		var modLine string
		if inRequireBlock {
			modLine = trimmed
		} else if strings.HasPrefix(trimmed, "require ") && !strings.Contains(trimmed, "(") {
			modLine = strings.TrimSpace(strings.TrimPrefix(trimmed, "require "))
		} else {
			continue
		}

		// Remove inline comments
		if idx := strings.Index(modLine, "//"); idx >= 0 {
			modLine = strings.TrimSpace(modLine[:idx])
		}

		parts := strings.Fields(modLine)
		if len(parts) < 1 {
			continue
		}

		modulePath := parts[0]
		// Skip stdlib (no dot in first segment before slash)
		domain := modulePath
		if idx := strings.Index(modulePath, "/"); idx > 0 {
			domain = modulePath[:idx]
		}
		if !strings.Contains(domain, ".") {
			continue
		}

		version := ""
		if len(parts) >= 2 {
			version = parts[1]
		}

		deps = append(deps, PackageManagerDep{
			Name:       modulePath,
			Version:    version,
			Ecosystem:  "go",
			SourceFile: relPath,
		})
	}
	return deps
}

func parsePackageJSONFile(content, relPath string) []PackageManagerDep {
	var deps []PackageManagerDep
	lines := strings.Split(content, "\n")

	inDepsSection := false
	braceDepth := 0
	depsStartDepth := -1

	depSectionRe := regexp.MustCompile(`^\s*"(?:dependencies|devDependencies|peerDependencies)"\s*:\s*\{`)
	depLineRe := regexp.MustCompile(`^\s*"(@?[\w][\w.\-/]*)"\s*:\s*"([^"]+)"`)

	for _, line := range lines {
		for _, ch := range line {
			if ch == '{' {
				braceDepth++
			} else if ch == '}' {
				braceDepth--
				if inDepsSection && braceDepth <= depsStartDepth {
					inDepsSection = false
					depsStartDepth = -1
				}
			}
		}

		if depSectionRe.MatchString(line) {
			inDepsSection = true
			depsStartDepth = braceDepth - 1
			continue
		}
		if !inDepsSection {
			continue
		}

		m := depLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		pkgName, version := m[1], m[2]
		if strings.HasPrefix(version, "workspace:") || strings.HasPrefix(version, "file:") ||
			strings.HasPrefix(version, "git+") || strings.HasPrefix(version, "github:") ||
			strings.HasPrefix(version, "link:") {
			continue
		}
		deps = append(deps, PackageManagerDep{Name: pkgName, Version: version, Ecosystem: "npm", SourceFile: relPath})
	}
	return deps
}

func extractPyPackageName(raw string) string {
	idx := strings.IndexAny(raw, "=<>!~[ #;")
	if idx < 0 {
		return strings.TrimSpace(raw)
	}
	return strings.TrimSpace(raw[:idx])
}

func parseRequirementsTxtFile(content, relPath string) []PackageManagerDep {
	var deps []PackageManagerDep
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "-") {
			continue
		}
		if idx := strings.Index(trimmed, "#"); idx >= 0 {
			trimmed = strings.TrimSpace(trimmed[:idx])
		}
		name := extractPyPackageName(trimmed)
		if name == "" {
			continue
		}
		deps = append(deps, PackageManagerDep{Name: name, Ecosystem: "pypi", SourceFile: relPath})
	}
	return deps
}

func parsePyprojectTomlFile(content, relPath string) []PackageManagerDep {
	var deps []PackageManagerDep
	lines := strings.Split(content, "\n")

	type section int
	const (
		sNone section = iota
		sProjectDeps
		sProjectOptionalDeps
		sPoetryDeps
		sDependencyGroups
	)

	currentSection := sNone
	inDepsArray := false

	poetryGroupRe := regexp.MustCompile(`^\[tool\.poetry\.group\.[^\]]+\.dependencies\]$`)
	keyValRe := regexp.MustCompile(`^([\w][\w\-._]*)\s*=`)
	arrayHeaderRe := regexp.MustCompile(`^\s*[\w][\w\-._]*\s*=\s*\[`)
	quotedRe := regexp.MustCompile(`^["']([^"']+)["']`)
	inlineQuotedRe := regexp.MustCompile(`"([^"]+)"`)
	projectDepsRe := regexp.MustCompile(`^\s*dependencies\s*=\s*\[`)

	addPyDep := func(raw string) {
		name := extractPyPackageName(raw)
		if name != "" && name != "python" {
			deps = append(deps, PackageManagerDep{Name: name, Ecosystem: "pypi", SourceFile: relPath})
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.HasPrefix(trimmed, "[") {
			inDepsArray = false
			switch {
			case trimmed == "[project]":
				currentSection = sProjectDeps
			case trimmed == "[project.optional-dependencies]":
				currentSection = sProjectOptionalDeps
			case trimmed == "[tool.poetry.dependencies]" || trimmed == "[tool.poetry.dev-dependencies]" || poetryGroupRe.MatchString(trimmed):
				currentSection = sPoetryDeps
			case trimmed == "[dependency-groups]":
				currentSection = sDependencyGroups
			default:
				currentSection = sNone
			}
			continue
		}

		switch currentSection {
		case sProjectDeps:
			if projectDepsRe.MatchString(line) {
				inDepsArray = true
				afterBracket := line[strings.Index(line, "[")+1:]
				if strings.Contains(afterBracket, "]") {
					inDepsArray = false
					for _, m := range inlineQuotedRe.FindAllStringSubmatch(afterBracket, -1) {
						addPyDep(m[1])
					}
				}
				continue
			}
			if inDepsArray {
				if trimmed == "]" {
					inDepsArray = false
					continue
				}
				if m := quotedRe.FindStringSubmatch(trimmed); m != nil {
					addPyDep(m[1])
				}
			}

		case sProjectOptionalDeps, sDependencyGroups:
			if arrayHeaderRe.MatchString(line) {
				inDepsArray = true
				afterBracket := line[strings.Index(line, "[")+1:]
				if strings.Contains(afterBracket, "]") {
					inDepsArray = false
					for _, m := range inlineQuotedRe.FindAllStringSubmatch(afterBracket, -1) {
						addPyDep(m[1])
					}
				}
				continue
			}
			if inDepsArray {
				if trimmed == "]" {
					inDepsArray = false
					continue
				}
				if strings.HasPrefix(trimmed, "{") {
					continue // skip include-group entries
				}
				if m := quotedRe.FindStringSubmatch(trimmed); m != nil {
					addPyDep(m[1])
				}
			}

		case sPoetryDeps:
			if m := keyValRe.FindStringSubmatch(trimmed); m != nil {
				name := m[1]
				if name != "python" {
					deps = append(deps, PackageManagerDep{Name: name, Ecosystem: "pypi", SourceFile: relPath})
				}
			}
		}
	}
	return deps
}

func parsePipfileFile(content, relPath string) []PackageManagerDep {
	var deps []PackageManagerDep
	inPackages := false
	keyValRe := regexp.MustCompile(`^([\w][\w\-._]*)\s*=`)

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			inPackages = trimmed == "[packages]" || trimmed == "[dev-packages]"
			continue
		}
		if !inPackages {
			continue
		}
		if strings.HasPrefix(trimmed, "python_version") || strings.HasPrefix(trimmed, "python_full_version") {
			continue
		}
		if m := keyValRe.FindStringSubmatch(trimmed); m != nil {
			deps = append(deps, PackageManagerDep{Name: m[1], Ecosystem: "pypi", SourceFile: relPath})
		}
	}
	return deps
}

func parseCargoTomlFile(content, relPath string) []PackageManagerDep {
	var deps []PackageManagerDep
	inDeps := false
	simpleRe := regexp.MustCompile(`^(\w[\w\-_]*)\s*=\s*"([^"]+)"`)
	tableRe := regexp.MustCompile(`^(\w[\w\-_]*)\s*=\s*\{\s*version\s*=\s*"([^"]+)"`)

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			inDeps = trimmed == "[dependencies]" || trimmed == "[dev-dependencies]"
			continue
		}
		if !inDeps {
			continue
		}
		if m := simpleRe.FindStringSubmatch(trimmed); m != nil {
			deps = append(deps, PackageManagerDep{Name: m[1], Version: m[2], Ecosystem: "cargo", SourceFile: relPath})
			continue
		}
		if m := tableRe.FindStringSubmatch(trimmed); m != nil {
			deps = append(deps, PackageManagerDep{Name: m[1], Version: m[2], Ecosystem: "cargo", SourceFile: relPath})
		}
	}
	return deps
}

func parseGemfileFile(content, relPath string) []PackageManagerDep {
	var deps []PackageManagerDep
	gemRe := regexp.MustCompile(`^\s*gem\s+['"]([^'"]+)['"]`)

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if m := gemRe.FindStringSubmatch(line); m != nil {
			deps = append(deps, PackageManagerDep{Name: m[1], Ecosystem: "rubygems", SourceFile: relPath})
		}
	}
	return deps
}

func parsePomXmlFile(content, relPath string) []PackageManagerDep {
	var deps []PackageManagerDep
	lines := strings.Split(content, "\n")

	inDepMgmt := false
	inDeps := false
	inDep := false
	var groupId, artifactId, version, scope string

	groupIdRe := regexp.MustCompile(`<groupId>([^<]+)</groupId>`)
	artifactIdRe := regexp.MustCompile(`<artifactId>([^<]+)</artifactId>`)
	versionRe := regexp.MustCompile(`<version>([^<]+)</version>`)
	scopeRe := regexp.MustCompile(`<scope>([^<]+)</scope>`)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "<dependencyManagement>") {
			inDepMgmt = true
			continue
		}
		if strings.HasPrefix(trimmed, "</dependencyManagement>") {
			inDepMgmt = false
			inDeps = false
			continue
		}
		if strings.HasPrefix(trimmed, "<dependencies>") {
			if !inDepMgmt {
				inDeps = true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "</dependencies>") {
			inDeps = false
			continue
		}
		if !inDeps {
			continue
		}
		if strings.HasPrefix(trimmed, "<dependency>") {
			inDep = true
			groupId, artifactId, version, scope = "", "", "", ""
			continue
		}
		if strings.HasPrefix(trimmed, "</dependency>") {
			if inDep && groupId != "" && artifactId != "" && scope != "test" {
				deps = append(deps, PackageManagerDep{
					Name:       groupId + ":" + artifactId,
					Version:    version,
					Ecosystem:  "maven",
					SourceFile: relPath,
				})
			}
			inDep = false
			groupId, artifactId, version, scope = "", "", "", ""
			continue
		}
		if inDep {
			if m := groupIdRe.FindStringSubmatch(trimmed); m != nil {
				groupId = m[1]
			} else if m := artifactIdRe.FindStringSubmatch(trimmed); m != nil {
				artifactId = m[1]
			} else if m := versionRe.FindStringSubmatch(trimmed); m != nil {
				version = m[1]
			} else if m := scopeRe.FindStringSubmatch(trimmed); m != nil {
				scope = m[1]
			}
		}
	}
	return deps
}

// parses a .csproj/.vbproj/.fsproj file
func parseCsProjFile(content, relPath string) []PackageManagerDep {
	var deps []PackageManagerDep
	pkgRefRe := regexp.MustCompile(`(?i)<PackageReference\s+Include="([^"]+)"(?:\s+Version="([^"]*)")?`)

	for _, line := range strings.Split(content, "\n") {
		if m := pkgRefRe.FindStringSubmatch(line); m != nil {
			deps = append(deps, PackageManagerDep{Name: m[1], Version: m[2], Ecosystem: "nuget", SourceFile: relPath})
		}
	}
	return deps
}

var gradleConfigs = []string{
	"implementation", "api", "compileOnly", "runtimeOnly",
	"testImplementation", "testCompileOnly", "testRuntimeOnly",
	"annotationProcessor", "kapt", "compile", "testCompile",
	"provided", "classpath", "optional",
	"testFixturesImplementation", "testFixturesApi",
	"integrationTestImplementation", "integrationTestRuntimeOnly",
}

// parses a build.gradle or build.gradle.kts
func parseGradleFile(content, relPath string) []PackageManagerDep {
	var deps []PackageManagerDep

	configsPat := strings.Join(gradleConfigs, "|")
	stringRe := regexp.MustCompile(`^\s*(?:` + configsPat + `)\s*[\(\s]['"]([^:'"]+):([^:'"]+)(?::[^'"]*)?['"]`)
	mapGroovyRe := regexp.MustCompile(`group\s*:\s*['"]([^'"]+)['"]\s*,\s*name\s*:\s*['"]([^'"]+)['"]`)
	mapKotlinRe := regexp.MustCompile(`group\s*=\s*["']([^"']+)["']\s*,\s*name\s*=\s*["']([^"']+)["']`)
	depsBlockRe := regexp.MustCompile(`^\s*dependencies\s*\{`)

	inDepsBlock := false
	braceDepth := 0
	depsStartDepth := -1

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, ch := range line {
			if ch == '{' {
				braceDepth++
			} else if ch == '}' {
				braceDepth--
				if inDepsBlock && braceDepth <= depsStartDepth {
					inDepsBlock = false
					depsStartDepth = -1
				}
			}
		}
		if depsBlockRe.MatchString(line) {
			inDepsBlock = true
			depsStartDepth = braceDepth - 1
			continue
		}
		if !inDepsBlock {
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "/*") {
			continue
		}
		if m := stringRe.FindStringSubmatch(line); m != nil {
			deps = append(deps, PackageManagerDep{Name: m[1] + ":" + m[2], Ecosystem: "maven", SourceFile: relPath})
			continue
		}
		if m := mapGroovyRe.FindStringSubmatch(line); m != nil {
			deps = append(deps, PackageManagerDep{Name: m[1] + ":" + m[2], Ecosystem: "maven", SourceFile: relPath})
			continue
		}
		if m := mapKotlinRe.FindStringSubmatch(line); m != nil {
			deps = append(deps, PackageManagerDep{Name: m[1] + ":" + m[2], Ecosystem: "maven", SourceFile: relPath})
		}
	}
	return deps
}

// parses a Gradle libs.versions.toml file
func parseVersionCatalogFile(content, relPath string) []PackageManagerDep {
	var deps []PackageManagerDep
	inLibraries := false
	moduleRe := regexp.MustCompile(`module\s*=\s*["']([^:'"]+):([^:'"]+)["']`)
	groupRe := regexp.MustCompile(`group\s*=\s*["']([^"']+)["']`)
	nameRe := regexp.MustCompile(`name\s*=\s*["']([^"']+)["']`)

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			inLibraries = trimmed == "[libraries]"
			continue
		}
		if !inLibraries {
			continue
		}
		if m := moduleRe.FindStringSubmatch(line); m != nil {
			deps = append(deps, PackageManagerDep{Name: m[1] + ":" + m[2], Ecosystem: "maven", SourceFile: relPath})
			continue
		}
		gm := groupRe.FindStringSubmatch(line)
		nm := nameRe.FindStringSubmatch(line)
		if gm != nil && nm != nil {
			deps = append(deps, PackageManagerDep{Name: gm[1] + ":" + nm[1], Ecosystem: "maven", SourceFile: relPath})
		}
	}
	return deps
}

// deduplicates by ecosystem:name
func dedupePkgManagerDeps(deps []PackageManagerDep) []PackageManagerDep {
	seen := make(map[string]bool)
	var result []PackageManagerDep
	for _, dep := range deps {
		key := dep.Ecosystem + ":" + dep.Name
		if !seen[key] {
			seen[key] = true
			result = append(result, dep)
		}
	}
	return result
}
