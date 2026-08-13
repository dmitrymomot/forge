// Package tests holds the rules docs/design.md states in prose, expressed so the
// build enforces them. It exports nothing and is never imported.
package tests

import (
	"encoding/json"
	"io"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const modulePath = "github.com/dmitrymomot/forge/"

// coreThirdParty names every third party a core/ package may import, test files
// included. core/ is the vocabulary the other domains agree on, so it carries no
// framework and no driver; testify is the repo-wide test framework and rides along.
var coreThirdParty = []string{
	"golang.org/x/text",
	"github.com/stretchr/testify",
}

// productDomains are the domains that own a feature. data/ provides connection
// factories and health checks, so it must never depend on one.
var productDomains = []string{
	"async", "auth", "comms", "finance", "ops", "realtime", "web", "view",
}

// thirdLevelDirs are the only reasons a package sits three levels deep: driver
// isolators, conformance suites, colocated codegen, and static data siblings.
var thirdLevelDirs = []string{
	"approvaltest", "brokertest", "cldr", "frankfurter", "gen", "markdown",
	"mmdb", "otel", "prometheus", "sentry", "storetest", "tlsprint",
}

// duplicateLeafNames are the leaf names design.md's uniqueness rule tolerates today.
// Both are conformance suites, which a consumer imports one of at a time; anything
// new landing here means a real import-alias collision.
var duplicateLeafNames = []string{"storetest"}

type listedPackage struct {
	ImportPath  string
	Name        string
	Imports     []string
	TestImports []string
}

// allImports covers the test files too: a dependency a package only takes in its
// tests still lands in go.mod and still crosses the layer.
func (p listedPackage) allImports() []string {
	all := make([]string, 0, len(p.Imports)+len(p.TestImports))
	all = append(all, p.Imports...)
	return append(all, p.TestImports...)
}

func (p listedPackage) rel() string { return strings.TrimPrefix(p.ImportPath, modulePath) }

func (p listedPackage) domain() string {
	dom, _, _ := strings.Cut(p.rel(), "/")
	return dom
}

// importDomain reports the forge domain an import path belongs to.
func importDomain(importPath string) string {
	dom, _, _ := strings.Cut(strings.TrimPrefix(importPath, modulePath), "/")
	return dom
}

// packages lists every package of the module once per test run.
func packages(t *testing.T) []listedPackage {
	t.Helper()
	cmd := exec.Command("go", "list", "-json", "../...")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}

	dec := json.NewDecoder(strings.NewReader(string(out)))
	var pkgs []listedPackage
	for {
		var p listedPackage
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		if strings.HasPrefix(p.ImportPath, modulePath) {
			pkgs = append(pkgs, p)
		}
	}
	if len(pkgs) == 0 {
		t.Fatal("go list returned no packages of this module")
	}
	return pkgs
}

func isForge(importPath string) bool { return strings.HasPrefix(importPath, modulePath) }

func isThirdParty(importPath string) bool {
	if isForge(importPath) {
		return false
	}
	first, _, _ := strings.Cut(importPath, "/")
	return strings.Contains(first, ".")
}

func TestCoreCarriesNoFrameworkAndNoDriver(t *testing.T) {
	for _, pkg := range packages(t) {
		if pkg.domain() != "core" {
			continue
		}
		for _, imported := range pkg.allImports() {
			switch {
			case isForge(imported):
				dom := importDomain(imported)
				if dom != "core" {
					t.Errorf("%s imports %s: core names no other domain", pkg.rel(), imported)
				}
			case isThirdParty(imported):
				allowed := slices.ContainsFunc(coreThirdParty, func(prefix string) bool {
					return strings.HasPrefix(imported, prefix)
				})
				if !allowed {
					t.Errorf("%s imports %s: core carries no framework", pkg.rel(), imported)
				}
			}
		}
	}
}

func TestCryptoNamesOnlyCoreAndItself(t *testing.T) {
	for _, pkg := range packages(t) {
		if pkg.domain() != "crypto" {
			continue
		}
		for _, imported := range pkg.allImports() {
			if !isForge(imported) {
				continue
			}
			dom := importDomain(imported)
			if dom != "crypto" && dom != "core" {
				t.Errorf("%s imports %s: crypto sits below every product domain", pkg.rel(), imported)
			}
		}
	}
}

// TestDataNamesNoProductDomain pins the anti-scope rule: data/ opens and health-checks
// connections. A dependency on a feature means schema or queries leaked into it.
func TestDataNamesNoProductDomain(t *testing.T) {
	for _, pkg := range packages(t) {
		if pkg.domain() != "data" {
			continue
		}
		for _, imported := range pkg.Imports {
			if !isForge(imported) {
				continue
			}
			dom := importDomain(imported)
			if slices.Contains(productDomains, dom) {
				t.Errorf("%s imports %s: data provides connections, never features", pkg.rel(), imported)
			}
		}
	}
}

func TestLayoutIsTwoLevelsDeep(t *testing.T) {
	for _, pkg := range packages(t) {
		rel := pkg.rel()
		if strings.HasPrefix(rel, "examples/") || rel == "tests" {
			continue
		}
		parts := strings.Split(rel, "/")
		switch {
		case len(parts) < 2:
			t.Errorf("%s: every package lives under a domain", rel)
		case len(parts) == 3 && !slices.Contains(thirdLevelDirs, parts[2]):
			t.Errorf("%s: a third level is only for a driver isolator, a conformance suite, or codegen", rel)
		case len(parts) > 3:
			t.Errorf("%s: two levels max", rel)
		}
	}
}

func TestLeafDirectoryIsThePackageName(t *testing.T) {
	for _, pkg := range packages(t) {
		rel := pkg.rel()
		if pkg.Name == "main" {
			continue
		}
		parts := strings.Split(rel, "/")
		if leaf := parts[len(parts)-1]; leaf != pkg.Name {
			t.Errorf("%s declares package %s: the leaf directory is the package name", rel, pkg.Name)
		}
	}
}

// TestLeafNamesAreUnique keeps imports free of forced aliasing: two packages with one
// name cannot both be imported plainly.
func TestLeafNamesAreUnique(t *testing.T) {
	seen := map[string]string{}
	for _, pkg := range packages(t) {
		rel := pkg.rel()
		if pkg.Name == "main" || rel == "tests" {
			continue
		}
		if slices.Contains(duplicateLeafNames, pkg.Name) {
			continue
		}
		if first, dup := seen[pkg.Name]; dup {
			t.Errorf("%s and %s both declare package %s", first, rel, pkg.Name)
			continue
		}
		seen[pkg.Name] = rel
	}
}

// TestTestkitStaysOutOfProductionCode keeps the testcontainers dependency tree out of
// any binary a consumer ships.
func TestTestkitStaysOutOfProductionCode(t *testing.T) {
	for _, pkg := range packages(t) {
		if pkg.domain() == "testkit" || strings.HasPrefix(pkg.rel(), "examples/") {
			continue
		}
		for _, imported := range pkg.Imports {
			if strings.HasPrefix(imported, modulePath+"testkit/") {
				t.Errorf("%s imports %s outside a test file", pkg.rel(), imported)
			}
		}
	}
}
