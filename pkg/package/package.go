// pkg/package/package.go
package pkg

import (
    "fmt"
    "strings"
)

// Core domain models
type Package struct {
    ID          string
    Name        string
    PackageType string
    Visibility  string
    Owner       Owner
    Versions    []Version
    Repository  *Repository
    Statistics  *Statistics
}

type Owner struct {
    Login     string
    Type      string
}

type Version struct {
    ID        string
    Name      string
    Metadata  map[string]interface{}
    Files     []File
    CreatedAt string
    UpdatedAt string
}

type File struct {
    Name    string
    Size    int
    SHA256  string
    URL     string
}

type Repository struct {
    Name     string
    FullName string
    URL      string
}

type Statistics struct {
    DownloadsCount int
}

// Package type definitions
type PackageType string

const (
    PackageTypeContainer PackageType = "container"
    PackageTypeNpm      PackageType = "npm"
    PackageTypeMaven    PackageType = "maven"
    PackageTypeNuGet    PackageType = "nuget"
    PackageTypeRubyGems PackageType = "rubygems"
)

// Validation types and interfaces
type ValidationError struct {
    PackageName string
    PackageType PackageType
    Message     string
}

func (e *ValidationError) Error() string {
    return fmt.Sprintf("validation error for %s package '%s': %s", e.PackageType, e.PackageName, e.Message)
}

type PackageValidator interface {
    ValidatePackage(pkg *Package) error
    ValidateVersion(ver *Version) error
    GetMaxFileSize() int64
    GetRequiredFiles() []string
}

// Package-type specific validators
type ContainerValidator struct{}

func (v *ContainerValidator) ValidatePackage(pkg *Package) error {
    if strings.Contains(pkg.Name, ":") {
        return &ValidationError{
            PackageName: pkg.Name,
            PackageType: PackageTypeContainer,
            Message:    "container name cannot contain ':' character",
        }
    }
    return nil
}

func (v *ContainerValidator) ValidateVersion(ver *Version) error {
    // Check for 10GB layer limit
    for _, file := range ver.Files {
        if file.Size > 10*1024*1024*1024 {
            return &ValidationError{
                PackageType: PackageTypeContainer,
                Message:    fmt.Sprintf("file %s exceeds 10GB layer limit", file.Name),
            }
        }
    }
    return nil
}

func (v *ContainerValidator) GetMaxFileSize() int64 {
    return 10 * 1024 * 1024 * 1024 // 10GB
}

func (v *ContainerValidator) GetRequiredFiles() []string {
    return []string{"manifest.json", "config.json"}
}

type NpmValidator struct{}

func (v *NpmValidator) ValidatePackage(pkg *Package) error {
    if !strings.HasPrefix(pkg.Name, "@") {
        return &ValidationError{
            PackageName: pkg.Name,
            PackageType: PackageTypeNpm,
            Message:    "npm package name must start with '@' for scoped packages",
        }
    }
    return nil
}

func (v *NpmValidator) ValidateVersion(ver *Version) error {
    hasPackageJson := false
    for _, file := range ver.Files {
        if file.Name == "package.json" {
            hasPackageJson = true
            break
        }
    }
    if !hasPackageJson {
        return &ValidationError{
            PackageType: PackageTypeNpm,
            Message:    "missing required package.json file",
        }
    }
    return nil
}

func (v *NpmValidator) GetMaxFileSize() int64 {
    return 256 * 1024 * 1024 // 256MB
}

func (v *NpmValidator) GetRequiredFiles() []string {
    return []string{"package.json"}
}

type MavenValidator struct{}

func (v *MavenValidator) ValidatePackage(pkg *Package) error {
    parts := strings.Split(pkg.Name, ":")
    if len(parts) != 2 {
        return &ValidationError{
            PackageName: pkg.Name,
            PackageType: PackageTypeMaven,
            Message:    "maven package name must be in format 'groupId:artifactId'",
        }
    }
    return nil
}

func (v *MavenValidator) ValidateVersion(ver *Version) error {
    hasPom := false
    for _, file := range ver.Files {
        if file.Name == "pom.xml" {
            hasPom = true
            break
        }
    }
    if !hasPom {
        return &ValidationError{
            PackageType: PackageTypeMaven,
            Message:    "missing required pom.xml file",
        }
    }
    return nil
}

func (v *MavenValidator) GetMaxFileSize() int64 {
    return 1024 * 1024 * 1024 // 1GB
}

func (v *MavenValidator) GetRequiredFiles() []string {
    return []string{"pom.xml"}
}

type NuGetValidator struct{}

func (v *NuGetValidator) ValidatePackage(pkg *Package) error {
    // NuGet package names must follow .NET namespace conventions
    if strings.ContainsAny(pkg.Name, "!@#$%^&*()+=[]{}|\\:;\"'<>?,/") {
        return &ValidationError{
            PackageName: pkg.Name,
            PackageType: PackageTypeNuGet,
            Message:    "invalid package name: must follow .NET namespace conventions",
        }
    }
    return nil
}

func (v *NuGetValidator) ValidateVersion(ver *Version) error {
    // Check for .nupkg file
    hasNupkg := false
    for _, file := range ver.Files {
        if strings.HasSuffix(file.Name, ".nupkg") {
            hasNupkg = true
            break
        }
    }
    if !hasNupkg {
        return &ValidationError{
            PackageType: PackageTypeNuGet,
            Message:    "missing required .nupkg file",
        }
    }

    // Check for nuspec file
    hasNuspec := false
    for _, file := range ver.Files {
        if strings.HasSuffix(file.Name, ".nuspec") {
            hasNuspec = true
            break
        }
    }
    if !hasNuspec {
        return &ValidationError{
            PackageType: PackageTypeNuGet,
            Message:    "missing required .nuspec file",
        }
    }

    return nil
}

func (v *NuGetValidator) GetMaxFileSize() int64 {
    return 250 * 1024 * 1024 // 250MB (common NuGet package limit)
}

func (v *NuGetValidator) GetRequiredFiles() []string {
    return []string{".nupkg", ".nuspec"}
}

type RubyGemsValidator struct{}

func (v *RubyGemsValidator) ValidatePackage(pkg *Package) error {
    // RubyGems naming conventions
    if strings.ContainsAny(pkg.Name, " !@#$%^&*()+=[]{}|\\:;\"'<>?,/") {
        return &ValidationError{
            PackageName: pkg.Name,
            PackageType: PackageTypeRubyGems,
            Message:    "invalid package name: can only contain alphanumeric characters, dashes, and underscores",
        }
    }

    // RubyGems doesn't allow uppercase letters in package names
    if strings.ToLower(pkg.Name) != pkg.Name {
        return &ValidationError{
            PackageName: pkg.Name,
            PackageType: PackageTypeRubyGems,
            Message:    "package name must be lowercase",
        }
    }

    return nil
}

func (v *RubyGemsValidator) ValidateVersion(ver *Version) error {
    // Check for .gem file
    hasGemFile := false
    for _, file := range ver.Files {
        if strings.HasSuffix(file.Name, ".gem") {
            hasGemFile = true
            break
        }
    }
    if !hasGemFile {
        return &ValidationError{
            PackageType: PackageTypeRubyGems,
            Message:    "missing required .gem file",
        }
    }

    // Check for gemspec
    hasGemspec := false
    for _, file := range ver.Files {
        if strings.HasSuffix(file.Name, ".gemspec") {
            hasGemspec = true
            break
        }
    }
    if !hasGemspec {
        return &ValidationError{
            PackageType: PackageTypeRubyGems,
            Message:    "missing required .gemspec file",
        }
    }

    // Validate metadata.gz size (from documentation)
    for _, file := range ver.Files {
        if strings.HasSuffix(file.Name, "metadata.gz") {
            if file.Size > 2*1024*1024 { // 2MB limit
                return &ValidationError{
                    PackageType: PackageTypeRubyGems,
                    Message:    "metadata.gz file must be less than 2MB",
                }
            }
        }
    }

    return nil
}

func (v *RubyGemsValidator) GetMaxFileSize() int64 {
    return 512 * 1024 * 1024 // 512MB (generous limit for gem files)
}

func (v *RubyGemsValidator) GetRequiredFiles() []string {
    return []string{".gem", ".gemspec"}
}

// Factory for getting the appropriate validator
func GetValidator(pkgType PackageType) (PackageValidator, error) {
    switch pkgType {
    case PackageTypeContainer:
        return &ContainerValidator{}, nil
    case PackageTypeNpm:
        return &NpmValidator{}, nil
    case PackageTypeMaven:
        return &MavenValidator{}, nil
    case PackageTypeNuGet:
        return &NuGetValidator{}, nil
    case PackageTypeRubyGems:
        return &RubyGemsValidator{}, nil
    default:
        return nil, fmt.Errorf("unsupported package type: %s", pkgType)
    }
}

// Helper function to validate a package
func ValidatePackage(pkg *Package) error {
    validator, err := GetValidator(PackageType(pkg.PackageType))
    if err != nil {
        return err
    }

    // Validate package-level constraints
    if err := validator.ValidatePackage(pkg); err != nil {
        return err
    }

    // Validate each version
    for _, version := range pkg.Versions {
        if err := validator.ValidateVersion(&version); err != nil {
            return err
        }
    }

    return nil
}
