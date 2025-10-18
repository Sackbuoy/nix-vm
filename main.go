package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"gopkg.in/yaml.v3"
)

type PackageSpec struct {
	Name    string
	Version string
}

type PackageInfo struct {
	Name    string
	Version string
	Commit  string
}

// Cache maps package names to version->commit mappings
type Cache map[string]map[string]string

func main() {
	// Get home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}

	nixVmDir := filepath.Join(homeDir, ".nix-vm")
	cacheFile := filepath.Join(nixVmDir, "cache.yaml")
	nixpkgsRepo := filepath.Join(nixVmDir, "nixpkgs")

	// Ensure ~/.nix-vm directory exists
	if err := os.MkdirAll(nixVmDir, 0755); err != nil {
		log.Fatalf("Failed to create %s: %v", nixVmDir, err)
	}

	// Ensure nixpkgs repo exists
	if err := ensureNixpkgsRepo(nixpkgsRepo); err != nil {
		log.Fatalf("Failed to setup nixpkgs repository: %v", err)
	}

	// Load packages from current directory
	packages, err := loadPackagesFromFile("nix-vm.yaml")
	if err != nil {
		log.Fatal(err)
	}

	// Load cache
	cache, err := loadCache(cacheFile)
	if err != nil {
		log.Printf("Warning: failed to load cache: %v", err)
		cache = make(Cache)
	}
	
	err = LaunchNixShell(nixpkgsRepo, packages, cache, cacheFile)
	if err != nil {
		log.Fatal(err)
	}
}

// ensureNixpkgsRepo checks for existing nixpkgs repos or clones if needed
func ensureNixpkgsRepo(repoPath string) error {
	// Check if the configured path already exists
	if isGitRepo(repoPath) {
		fmt.Printf("Nixpkgs repository found at: %s\n", repoPath)
		return nil
	}

	// Try to find an existing nixpkgs git repository
	fmt.Println("Looking for existing nixpkgs repository...")
	
	homeDir, _ := os.UserHomeDir()
	commonPaths := []string{
		filepath.Join(homeDir, "nixpkgs"),
		filepath.Join(homeDir, "src", "nixpkgs"),
		filepath.Join(homeDir, "dev", "nixpkgs"),
		filepath.Join(homeDir, "projects", "nixpkgs"),
		filepath.Join(homeDir, "Dev", "nixpkgs"),
		filepath.Join(homeDir, "code", "nixpkgs"),
		"/etc/nixos/nixpkgs",
	}

	// Also check NIX_PATH for nixpkgs location
	if nixPath := os.Getenv("NIX_PATH"); nixPath != "" {
		for _, entry := range strings.Split(nixPath, ":") {
			if strings.HasPrefix(entry, "nixpkgs=") {
				path := strings.TrimPrefix(entry, "nixpkgs=")
				commonPaths = append([]string{path}, commonPaths...)
			}
		}
	}

	for _, path := range commonPaths {
		if isGitRepo(path) {
			fmt.Printf("Found existing nixpkgs repository at: %s\n", path)
			fmt.Printf("Creating symlink: %s -> %s\n", repoPath, path)
			
			// Create parent directory if needed
			if err := os.MkdirAll(filepath.Dir(repoPath), 0755); err != nil {
				return err
			}
			
			// Create symlink to existing repo
			if err := os.Symlink(path, repoPath); err != nil {
				return fmt.Errorf("failed to create symlink: %w", err)
			}
			return nil
		}
	}

	// No existing repo found, clone it
	fmt.Printf("No existing nixpkgs repository found. Cloning to %s...\n", repoPath)
	fmt.Println("This may take a while (nixpkgs is a large repository)...")

	cmd := exec.Command("git", "clone", 
		"--depth", "1",
		"https://github.com/NixOS/nixpkgs.git", 
		repoPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to clone nixpkgs: %w", err)
	}

	fmt.Println("Successfully cloned nixpkgs repository")
	
	fmt.Println("Fetching full repository history...")
	cmd = exec.Command("git", "fetch", "--unshallow")
	cmd.Dir = repoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to unshallow repository: %w", err)
	}

	fmt.Println("Repository setup complete")
	return nil
}

// isGitRepo checks if a path is a git repository
func isGitRepo(path string) bool {
	gitDir := filepath.Join(path, ".git")
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		return true
	}
	return false
}

// loadCache loads the cache from disk
func loadCache(filename string) (Cache, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return make(Cache), nil
		}
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	cache := make(Cache)
	if err := yaml.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("failed to parse cache YAML: %w", err)
	}

	return cache, nil
}

// saveCache saves the cache to disk
func saveCache(filename string, cache Cache) error {
	data, err := yaml.Marshal(cache)
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	return nil
}

// getCachedCommit checks if a package version is in the cache
func getCachedCommit(cache Cache, packageName, version string) (string, bool) {
	if versions, ok := cache[packageName]; ok {
		if commit, ok := versions[version]; ok {
			return commit, true
		}
	}
	return "", false
}

// setCachedCommit adds a package version to the cache
func setCachedCommit(cache Cache, packageName, version, commit string) {
	if cache[packageName] == nil {
		cache[packageName] = make(map[string]string)
	}
	cache[packageName][version] = commit
}

// loadPackagesFromFile reads package specifications from a YAML file
func loadPackagesFromFile(filename string) ([]PackageSpec, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Parse YAML into a map
	packageMap := make(map[string]string)
	if err := yaml.Unmarshal(data, &packageMap); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Convert map to slice of PackageSpec
	var packages []PackageSpec
	for name, version := range packageMap {
		packages = append(packages, PackageSpec{
			Name:    name,
			Version: version,
		})
	}

	return packages, nil
}

// generateVersionsNix creates a versions flake directory for use in other flakes
func generateVersionsNix(packages []PackageInfo) error {
	// Create versions directory
	if err := os.MkdirAll("versions", 0755); err != nil {
		return fmt.Errorf("failed to create versions directory: %w", err)
	}
	
	var sb strings.Builder
	
	sb.WriteString("# Auto-generated by nix-vm\n")
	sb.WriteString("# Import this in your flake to get version-pinned packages\n")
	sb.WriteString("{\n")
	sb.WriteString("  description = \"Version-pinned packages\";\n\n")
	sb.WriteString("  inputs = {\n")
	
	// Generate inputs for each package
	for _, pkg := range packages {
		sb.WriteString(fmt.Sprintf("    nixpkgs-%s.url = \"github:NixOS/nixpkgs/%s\";\n", pkg.Name, pkg.Commit))
	}
	
	sb.WriteString("  };\n\n")
	sb.WriteString("  outputs = { self")
	
	// Add all inputs to outputs parameters
	for _, pkg := range packages {
		sb.WriteString(fmt.Sprintf(", nixpkgs-%s", pkg.Name))
	}
	sb.WriteString(" }: {\n")
	sb.WriteString("    packages = system: {\n")
	
	// Export each package
	for _, pkg := range packages {
		sb.WriteString(fmt.Sprintf("      %s = nixpkgs-%s.legacyPackages.${system}.%s; # version %s\n", 
			pkg.Name, pkg.Name, pkg.Name, pkg.Version))
	}
	
	sb.WriteString("    };\n")
	sb.WriteString("  };\n")
	sb.WriteString("}\n")
	
	// Write to versions/flake.nix
	flakePath := filepath.Join("versions", "flake.nix")
	if err := os.WriteFile(flakePath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("failed to write flake.nix: %w", err)
	}
	
	fmt.Printf("Generated versions/flake.nix with %d package(s)\n", len(packages))
	return nil
}

// FindPackageVersion searches a nixpkgs git repository for a package version
// and returns the commit hash for that version.
func FindPackageVersion(repoPath, packageName, desiredVersion string, cache Cache) (string, error) {
	// Check cache first
	if commit, found := getCachedCommit(cache, packageName, desiredVersion); found {
		fmt.Printf("  (found in cache)\n")
		return commit, nil
	}

	fmt.Printf("  (searching git repository...)\n")

	// Find the package file path
	packagePath, err := findPackageFile(repoPath, packageName)
	if err != nil {
		return "", fmt.Errorf("failed to find package file: %w", err)
	}

	// Get all commits that modified this file
	commits, err := getCommitsForFile(repoPath, packagePath)
	if err != nil {
		return "", fmt.Errorf("failed to get commits: %w", err)
	}

	// Track all versions found with their commit info
	type versionInfo struct {
		version string
		commit  string
		date    string
	}
	var foundVersions []versionInfo
	seenVersions := make(map[string]bool)
	versionRegex := regexp.MustCompile(`version\s*=\s*"([^"]+)"`)
	
	for _, commit := range commits {
		version, err := getVersionFromFileAtCommit(repoPath, commit, packagePath, versionRegex)
		if err != nil {
			continue
		}
		
		if version == desiredVersion {
			// Cache the result
			setCachedCommit(cache, packageName, desiredVersion, commit)
			return commit, nil
		}
		
		// Store unique versions in order encountered (newest first)
		if !seenVersions[version] {
			seenVersions[version] = true
			date, _ := getCommitDate(repoPath, commit)
			foundVersions = append(foundVersions, versionInfo{
				version: version,
				commit:  commit[:7], // short hash
				date:    date,
			})
		}
	}

	// Version not found, show available versions
	if len(foundVersions) > 0 {
		fmt.Printf("\n❌ Version %s not found for package %s\n\n", desiredVersion, packageName)
		fmt.Printf("Available versions (showing up to 30 most recent):\n")
		fmt.Printf("%-20s %-12s %s\n", "Version", "Commit", "Date")
		fmt.Printf("%s\n", strings.Repeat("-", 60))
		
		limit := 30
		if len(foundVersions) < limit {
			limit = len(foundVersions)
		}
		
		for i := 0; i < limit; i++ {
			v := foundVersions[i]
			fmt.Printf("%-20s %-12s %s\n", v.version, v.commit, v.date)
		}
		
		if len(foundVersions) > limit {
			fmt.Printf("\n... and %d more versions (total: %d)\n", len(foundVersions)-limit, len(foundVersions))
		}
		fmt.Println()
	}

	return "", fmt.Errorf("version %s not found for package %s", desiredVersion, packageName)
}

// getCommitDate returns the date of a commit
func getCommitDate(repoPath, commit string) (string, error) {
	cmd := exec.Command("git", "show", "-s", "--format=%ci", commit)
	cmd.Dir = repoPath
	
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	
	// Parse date and return just the date part (YYYY-MM-DD)
	dateStr := strings.TrimSpace(string(output))
	if len(dateStr) >= 10 {
		return dateStr[:10], nil
	}
	return dateStr, nil
}

// LaunchNixShell finds the package versions and launches a nix shell with them
func LaunchNixShell(repoPath string, packages []PackageSpec, cache Cache, cacheFile string) error {
	var flakeRefs []string
	var packageInfos []PackageInfo
	cacheModified := false
	
	for _, pkg := range packages {
		fmt.Printf("Searching for %s version %s...\n", pkg.Name, pkg.Version)
		
		commit, err := FindPackageVersion(repoPath, pkg.Name, pkg.Version, cache)
		if err != nil {
			return fmt.Errorf("package %s: %w", pkg.Name, err)
		}
		
		nixpkgsURL := fmt.Sprintf("github:nixos/nixpkgs/%s#%s", commit, pkg.Name)
		fmt.Printf("Found %s version %s at: %s\n", pkg.Name, pkg.Version, nixpkgsURL)
		flakeRefs = append(flakeRefs, nixpkgsURL)
		
		packageInfos = append(packageInfos, PackageInfo{
			Name:    pkg.Name,
			Version: pkg.Version,
			Commit:  commit,
		})
		
		cacheModified = true
	}

	// Generate versions.nix file
	if err := generateVersionsNix(packageInfos); err != nil {
		log.Printf("Warning: failed to generate versions.nix: %v", err)
	}

	// Save cache if it was modified
	if cacheModified {
		if err := saveCache(cacheFile, cache); err != nil {
			log.Printf("Warning: failed to save cache: %v", err)
		} else {
			fmt.Printf("Cache updated: %s\n", cacheFile)
		}
	}

	fmt.Printf("\nLaunching nix shell with %d package(s)...\n", len(packages))

	// Prepare nix shell command with all packages
	args := []string{"nix", "shell"}
	args = append(args, flakeRefs...)

	// Find nix binary
	nixBinPath, err := exec.LookPath("nix")
	if err != nil {
		return fmt.Errorf("nix not found: %w", err)
	}

	// Replace current process with nix
	return syscall.Exec(nixBinPath, args, os.Environ())
}

func findPackageFile(repoPath, packageName string) (string, error) {
	cmd := exec.Command("git", "grep", "-l", fmt.Sprintf(`pname = "%s"`, packageName), "HEAD", "--", "pkgs/")
	cmd.Dir = repoPath
	
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 {
		return "", fmt.Errorf("package not found")
	}

	// Return the first match (remove "HEAD:" prefix if present)
	path := strings.TrimPrefix(lines[0], "HEAD:")
	return path, nil
}

func getCommitsForFile(repoPath, filePath string) ([]string, error) {
	cmd := exec.Command("git", "rev-list", "HEAD", "--", filePath)
	cmd.Dir = repoPath

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	commits := strings.Split(strings.TrimSpace(string(output)), "\n")
	return commits, nil
}

// getVersionFromFileAtCommit reads the actual file contents at a specific commit
// and extracts the version field
func getVersionFromFileAtCommit(repoPath, commit, filePath string, versionRegex *regexp.Regexp) (string, error) {
	// Use git show to get the file contents at this commit
	cmd := exec.Command("git", "show", fmt.Sprintf("%s:%s", commit, filePath))
	cmd.Dir = repoPath

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get file at commit: %w", err)
	}

	// Search for version in the file contents
	matches := versionRegex.FindSubmatch(output)
	if len(matches) < 2 {
		return "", fmt.Errorf("version not found in file")
	}

	return string(matches[1]), nil
}
