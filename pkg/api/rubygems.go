package api

import (
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
)

// RubyGems specification structure
type GemSpec struct {
    Name         string
    Version      string
    Platform     string
    Authors      []string
    Summary      string
    Description  string
    Homepage     string
    Licenses     []string
    Dependencies []GemDependency
}

type GemDependency struct {
    Name                string
    Requirement         string
    Type               string // runtime, development, etc.
    Prerelease         bool
    DevelopmentAllowed bool
}

func parseGemspec(gemFile string) (*GemSpec, error) {
    // Get the gemspec from the .gem file
    tmpDir, err := os.MkdirTemp("", "gem-extract-*")
    if err != nil {
        return nil, fmt.Errorf("failed to create temp directory: %v", err)
    }
    defer os.RemoveAll(tmpDir)

    // Extract the gemspec using gem specification
    cmd := exec.Command("gem", "spec", gemFile)
    output, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("failed to extract gemspec: %v", err)
    }

    // Parse the YAML output
    var spec GemSpec
    if err := yaml.Unmarshal(output, &spec); err != nil {
        return nil, fmt.Errorf("failed to parse gemspec: %v", err)
    }

    // Validate required fields
    if spec.Name == "" {
        return nil, fmt.Errorf("gemspec missing required field: name")
    }
    if spec.Version == "" {
        return nil, fmt.Errorf("gemspec missing required field: version")
    }

    return &spec, nil
}

func validateGemMetadata(gemFile string) error {
    // Check metadata.gz size limit (2MB)
    metadataFile := filepath.Join(filepath.Dir(gemFile), "metadata.gz")
    info, err := os.Stat(metadataFile)
    if err == nil && info.Size() > 2*1024*1024 {
        return fmt.Errorf("metadata.gz exceeds size limit of 2MB")
    }

    return nil
}

func (a *API) publishGem(opts UploadOptions, gemFile string) error {
    // Parse and validate .gem file
    spec, err := parseGemspec(gemFile)
    if err != nil {
        return err
    }

    // Validate metadata size
    if err := validateGemMetadata(gemFile); err != nil {
        return err
    }

    // Update gem metadata
    spec.Homepage = fmt.Sprintf("https://github.com/%s/%s", opts.Organization, spec.Name)

    // For gems, we need to handle both the gem file and its metadata
    files := []string{gemFile}
    
    // Check for and include metadata files
    metadataFile := strings.TrimSuffix(gemFile, ".gem") + ".metadata.gz"
    if _, err := os.Stat(metadataFile); err == nil {
        files = append(files, metadataFile)
    }

    return nil
}

// Helper function to build correct gem repository path
func buildGemPath(name, version string) string {
    // RubyGems uses a specific directory structure
    // gems/[a-z]/[NAME]/[NAME]-[VERSION].gem
    firstChar := strings.ToLower(name[0:1])
    return filepath.Join("gems", firstChar, name, fmt.Sprintf("%s-%s.gem", name, version))
}
