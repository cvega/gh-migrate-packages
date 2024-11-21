package api

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "mime/multipart"
    "net/http"
    "os"
    "path/filepath"
    "strings"

    "github.com/cvega/gh-migrate-packages/pkg/package"
)

// UploadOptions contains parameters for package uploads
type UploadOptions struct {
    Organization string
    PackageName  string
    Version      string
    PackageType  string
    Metadata     map[string]interface{}
    Files        []string
    Visibility   string // "public" or "private"
}

// Upload error types for specific handling
type (
    ErrPackageExists struct {
        PackageName string
    }
    ErrVersionExists struct {
        PackageName string
        Version     string
    }
    ErrUploadFailed struct {
        Cause error
    }
)

func (e ErrPackageExists) Error() string {
    return fmt.Sprintf("package %s already exists", e.PackageName)
}

func (e ErrVersionExists) Error() string {
    return fmt.Sprintf("version %s of package %s already exists", e.Version, e.PackageName)
}

func (e ErrUploadFailed) Error() string {
    return fmt.Sprintf("upload failed: %v", e.Cause)
}

// UploadPackageVersion handles the upload process for different package types
func (a *API) UploadPackageVersion(opts UploadOptions) error {
    // Validate package type
    validator, err := pkg.GetValidator(pkg.PackageType(opts.PackageType))
    if err != nil {
        return fmt.Errorf("invalid package type: %v", err)
    }

    // Check file size limits
    maxSize := validator.GetMaxFileSize()
    for _, filePath := range opts.Files {
        info, err := os.Stat(filePath)
        if err != nil {
            return fmt.Errorf("failed to stat file %s: %v", filePath, err)
        }
        if info.Size() > maxSize {
            return fmt.Errorf("file %s exceeds maximum size of %d bytes", filePath, maxSize)
        }
    }

    // Handle upload based on package type
    switch pkg.PackageType(opts.PackageType) {
    case pkg.PackageTypeContainer:
        return a.uploadContainer(opts)
    case pkg.PackageTypeNpm:
        return a.uploadNpm(opts)
    case pkg.PackageTypeMaven:
        return a.uploadMaven(opts)
    case pkg.PackageTypeNuGet:
        return a.uploadNuGet(opts)
    case pkg.PackageTypeRubyGems:
        return a.uploadRubyGems(opts)
    default:
        return fmt.Errorf("unsupported package type: %s", opts.PackageType)
    }
}

func (a *API) uploadContainer(opts UploadOptions) error {
    // Container registry uses a different endpoint structure
    baseURL := fmt.Sprintf("https://ghcr.io/v2/%s/%s", opts.Organization, opts.PackageName)

    // Check for manifest existence
    manifestURL := fmt.Sprintf("%s/manifests/%s", baseURL, opts.Version)
    exists, err := a.checkExists(manifestURL)
    if err != nil {
        return err
    }
    if exists && !opts.Force {
        return &ErrVersionExists{PackageName: opts.PackageName, Version: opts.Version}
    }

    // Upload each layer
    layers := []string{}
    for _, file := range opts.Files {
        if strings.HasSuffix(file, ".tar.gz") {
            digest, err := a.uploadContainerLayer(baseURL, file)
            if err != nil {
                return fmt.Errorf("failed to upload layer %s: %v", file, err)
            }
            layers = append(layers, digest)
        }
    }

    // Upload manifest
    manifest := generateContainerManifest(layers, opts)
    if err := a.uploadContainerManifest(manifestURL, manifest); err != nil {
        return fmt.Errorf("failed to upload manifest: %v", err)
    }

    return nil
}

func (a *API) uploadNpm(opts UploadOptions) error {
    // NPM packages require the package.json and .tgz file
    var packageJSON, tarball string
    for _, file := range opts.Files {
        switch {
        case strings.HasSuffix(file, "package.json"):
            packageJSON = file
        case strings.HasSuffix(file, ".tgz"):
            tarball = file
        }
    }

    if packageJSON == "" || tarball == "" {
        return fmt.Errorf("missing required npm package files")
    }

    // Construct the upload URL for npm packages
    url := fmt.Sprintf("https://npm.pkg.github.com/%s", opts.Organization)

    // Create multipart form data
    body := &bytes.Buffer{}
    writer := multipart.NewWriter(body)

    // Add package.json
    pkgFile, err := os.Open(packageJSON)
    if err != nil {
        return err
    }
    defer pkgFile.Close()

    part, err := writer.CreateFormFile("package", filepath.Base(packageJSON))
    if err != nil {
        return err
    }
    if _, err := io.Copy(part, pkgFile); err != nil {
        return err
    }

    // Add tarball
    tarFile, err := os.Open(tarball)
    if err != nil {
        return err
    }
    defer tarFile.Close()

    part, err = writer.CreateFormFile("tarball", filepath.Base(tarball))
    if err != nil {
        return err
    }
    if _, err := io.Copy(part, tarFile); err != nil {
        return err
    }

    writer.Close()

    // Create request
    req, err := http.NewRequestWithContext(a.ctx, "PUT", url, body)
    if err != nil {
        return err
    }

    req.Header.Set("Content-Type", writer.FormDataContentType())
    req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.token))

    // Send request
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
        return fmt.Errorf("npm upload failed with status: %s", resp.Status)
    }

    return nil
}

func (a *API) uploadMaven(opts UploadOptions) error {
    // Maven requires specific handling for pom.xml and jar files
    var pomFile, jarFile string
    for _, file := range opts.Files {
        switch {
        case strings.HasSuffix(file, "pom.xml"):
            pomFile = file
        case strings.HasSuffix(file, ".jar"):
            jarFile = file
        }
    }

    if pomFile == "" {
        return fmt.Errorf("missing required pom.xml file")
    }

    // Parse group and artifact IDs from pom.xml
    groupID, artifactID, err := parseMavenPOM(pomFile)
    if err != nil {
        return err
    }

    // Construct Maven repository URL
    baseURL := fmt.Sprintf("https://maven.pkg.github.com/%s/%s/%s/%s",
        opts.Organization, groupID, artifactID, opts.Version)

    // Upload POM
    if err := a.uploadMavenFile(baseURL+"/pom.xml", pomFile); err != nil {
        return err
    }

    // Upload JAR if present
    if jarFile != "" {
        if err := a.uploadMavenFile(baseURL+"/"+filepath.Base(jarFile), jarFile); err != nil {
            return err
        }
    }

    return nil
}

func (a *API) uploadNuGet(opts UploadOptions) error {
    // NuGet requires .nupkg file
    var nupkgFile string
    for _, file := range opts.Files {
        if strings.HasSuffix(file, ".nupkg") {
            nupkgFile = file
            break
        }
    }

    if nupkgFile == "" {
        return fmt.Errorf("missing required .nupkg file")
    }

    // Construct NuGet push URL
    url := fmt.Sprintf("https://nuget.pkg.github.com/%s/upload", opts.Organization)

    // Create multipart form data
    body := &bytes.Buffer{}
    writer := multipart.NewWriter(body)

    // Add .nupkg file
    file, err := os.Open(nupkgFile)
    if err != nil {
        return err
    }
    defer file.Close()

    part, err := writer.CreateFormFile("package", filepath.Base(nupkgFile))
    if err != nil {
        return err
    }
    if _, err := io.Copy(part, file); err != nil {
        return err
    }

    writer.Close()

    // Create request
    req, err := http.NewRequestWithContext(a.ctx, "PUT", url, body)
    if err != nil {
        return err
    }

    req.Header.Set("Content-Type", writer.FormDataContentType())
    req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.token))

    // Send request
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
        return fmt.Errorf("nuget upload failed with status: %s", resp.Status)
    }

    return nil
}

func (a *API) uploadRubyGems(opts UploadOptions) error {
    // RubyGems requires .gem file
    var gemFile string
    for _, file := range opts.Files {
        if strings.HasSuffix(file, ".gem") {
            gemFile = file
            break
        }
    }

    if gemFile == "" {
        return fmt.Errorf("missing required .gem file")
    }

    // Construct RubyGems push URL
    url := fmt.Sprintf("https://rubygems.pkg.github.com/%s/api/v1/gems", opts.Organization)

    // Open and read the .gem file
    file, err := os.Open(gemFile)
    if err != nil {
        return err
    }
    defer file.Close()

    // Create request
    req, err := http.NewRequestWithContext(a.ctx, "POST", url, file)
    if err != nil {
        return err
    }

    req.Header.Set("Content-Type", "application/octet-stream")
    req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.token))

    // Send request
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
        return fmt.Errorf("rubygems upload failed with status: %s", resp.Status)
    }

    return nil
}

// Helper functions for container registry
func (a *API) uploadContainerLayer(baseURL, file string) (string, error) {
    // Implementation for container layer upload
    // This would handle the actual upload of container layers
    return "", nil // TODO: Implement
}

func generateContainerManifest(layers []string, opts UploadOptions) map[string]interface{} {
    // Implementation to generate container manifest
    return nil // TODO: Implement
}

func (a *API) uploadContainerManifest(url string, manifest map[string]interface{}) error {
    // Implementation for manifest upload
    return nil // TODO: Implement
}

// Helper function for Maven
func parseMavenPOM(pomFile string) (groupID, artifactID string, err error) {
    // Implementation to parse Maven POM file
    return "", "", nil // TODO: Implement
}

func (a *API) uploadMavenFile(url, file string) error {
    // Implementation for Maven file upload
    return nil // TODO: Implement
}

// Helper function to check if resource exists
func (a *API) checkExists(url string) (bool, error) {
    req, err := http.NewRequestWithContext(a.ctx, "HEAD", url, nil)
    if err != nil {
        return false, err
    }

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return false, err
    }
    defer resp.Body.Close()

    return resp.StatusCode == http.StatusOK, nil
}

