// pkg/api/upload.go

package api

import (
    "context"
    "fmt"
    "io"
    "path/filepath"
    "sync"
    "time"
)

// UploadManager handles package uploads across different registries
type UploadManager struct {
    client      *API
    maxRetries  int
    retryDelay  time.Duration
    concurrency int
}

func NewUploadManager(client *API) *UploadManager {
    return &UploadManager{
        client:      client,
        maxRetries:  3,
        retryDelay:  5 * time.Second,
        concurrency: 5,
    }
}

// UploadResult tracks the status of an upload operation
type UploadResult struct {
    PackageName string
    Version     string
    Success     bool
    Error       error
}

// retryableUpload attempts to upload with retries
func (m *UploadManager) retryableUpload(ctx context.Context, fn func() error) error {
    var lastErr error
    for attempt := 0; attempt < m.maxRetries; attempt++ {
        if err := fn(); err != nil {
            lastErr = err
            select {
            case <-ctx.Done():
                return ctx.Err()
            case <-time.After(m.retryDelay * time.Duration(attempt+1)):
                continue
            }
        }
        return nil
    }
    return fmt.Errorf("upload failed after %d attempts: %v", m.maxRetries, lastErr)
}

// ContainerUpload handles Docker/OCI container uploads
func (m *UploadManager) ContainerUpload(ctx context.Context, opts UploadOptions) error {
    // Validate container-specific requirements
    if err := validateContainerUpload(opts); err != nil {
        return fmt.Errorf("container validation failed: %w", err)
    }

    // Upload layers concurrently
    var wg sync.WaitGroup
    layerResults := make(chan string, len(opts.Files))
    layerErrors := make(chan error, len(opts.Files))
    
    sem := make(chan struct{}, m.concurrency)
    for _, file := range opts.Files {
        if !isContainerLayer(file) {
            continue
        }

        wg.Add(1)
        sem <- struct{}{} // Acquire semaphore
        
        go func(layerFile string) {
            defer wg.Done()
            defer func() { <-sem }() // Release semaphore
            
            digest, err := m.client.uploadContainerLayer(
                fmt.Sprintf("%s/%s", opts.Organization, opts.PackageName),
                layerFile,
            )
            if err != nil {
                layerErrors <- fmt.Errorf("layer upload failed: %w", err)
                return
            }
            layerResults <- digest
        }(file)
    }

    wg.Wait()
    close(layerResults)
    close(layerErrors)

    // Check for any layer upload errors
    if len(layerErrors) > 0 {
        return <-layerErrors
    }

    // Collect layer digests
    var layers []string
    for digest := range layerResults {
        layers = append(layers, digest)
    }

    // Generate and upload manifest
    manifest, err := generateContainerManifest(layers, opts)
    if err != nil {
        return fmt.Errorf("manifest generation failed: %w", err)
    }

    manifestURL := fmt.Sprintf("%s/%s/manifests/%s",
        opts.Organization, opts.PackageName, opts.Version)
    
    return m.retryableUpload(ctx, func() error {
        return m.client.uploadContainerManifest(manifestURL, manifest)
    })
}

// NPMUpload handles NPM package uploads
func (m *UploadManager) NPMUpload(ctx context.Context, opts UploadOptions) error {
    // Find package.json and tarball
    var pkgJSON, tarball string
    for _, file := range opts.Files {
        switch {
        case filepath.Base(file) == "package.json":
            pkgJSON = file
        case filepath.Ext(file) == ".tgz":
            tarball = file
        }
    }

    if pkgJSON == "" || tarball == "" {
        return fmt.Errorf("missing required npm files: package.json and/or .tgz")
    }

    // Parse package.json
    pkg, err := parseNPMPackage(pkgJSON)
    if err != nil {
        return fmt.Errorf("failed to parse package.json: %w", err)
    }

    // Verify version matches
    if pkg.Version != opts.Version {
        return fmt.Errorf("version mismatch: package.json has %s, expected %s",
            pkg.Version, opts.Version)
    }

    // Upload package
    return m.retryableUpload(ctx, func() error {
        return m.client.uploadNPMFiles(opts.Organization, pkg.Name, opts.Version,
            map[string]string{
                "package.json": pkgJSON,
                "package.tgz": tarball,
            })
    })
}

// MavenUpload handles Maven artifact uploads
func (m *UploadManager) MavenUpload(ctx context.Context, opts UploadOptions) error {
    // Find POM and JAR files
    var pomFile string
    var jarFiles []string
    for _, file := range opts.Files {
        switch {
        case filepath.Ext(file) == ".pom":
            pomFile = file
        case filepath.Ext(file) == ".jar":
            jarFiles = append(jarFiles, file)
        }
    }

    if pomFile == "" {
        return fmt.Errorf("missing required pom.xml file")
    }

    // Parse POM file
    groupID, artifactID, err := parseMavenPOM(pomFile)
    if err != nil {
        return fmt.Errorf("failed to parse POM: %w", err)
    }

    // Upload POM first
    if err := m.retryableUpload(ctx, func() error {
        return m.client.uploadMavenFile(
            fmt.Sprintf("%s/%s/%s/%s/pom.xml",
                opts.Organization, groupID, artifactID, opts.Version),
            pomFile,
        )
    }); err != nil {
        return err
    }

    // Upload JARs concurrently
    var wg sync.WaitGroup
    uploadErrors := make(chan error, len(jarFiles))
    
    sem := make(chan struct{}, m.concurrency)
    for _, jar := range jarFiles {
        wg.Add(1)
        sem <- struct{}{} // Acquire semaphore
        
        go func(jarFile string) {
            defer wg.Done()
            defer func() { <-sem }() // Release semaphore
            
            err := m.retryableUpload(ctx, func() error {
                return m.client.uploadMavenFile(
                    fmt.Sprintf("%s/%s/%s/%s/%s",
                        opts.Organization, groupID, artifactID, opts.Version,
                        filepath.Base(jarFile)),
                    jarFile,
                )
            })
            if err != nil {
                uploadErrors <- err
            }
        }(jar)
    }

    wg.Wait()
    close(uploadErrors)

    // Check for any upload errors
    if len(uploadErrors) > 0 {
        return <-uploadErrors
    }

    return nil
}

// NuGetUpload handles NuGet package uploads
func (m *UploadManager) NuGetUpload(ctx context.Context, opts UploadOptions) error {
    // Find .nupkg file
    var nupkgFile string
    for _, file := range opts.Files {
        if filepath.Ext(file) == ".nupkg" {
            nupkgFile = file
            break
        }
    }

    if nupkgFile == "" {
        return fmt.Errorf("missing required .nupkg file")
    }

    // Parse and validate .nupkg
    manifest, err := parseNuspec(nupkgFile)
    if err != nil {
        return fmt.Errorf("failed to parse .nupkg: %w", err)
    }

    // Verify version matches
    if manifest.Metadata.Version != opts.Version {
        return fmt.Errorf("version mismatch: .nupkg has %s, expected %s",
            manifest.Metadata.Version, opts.Version)
    }

    // Upload package
    return m.retryableUpload(ctx, func() error {
        return m.client.uploadNuGetPackage(opts.Organization, nupkgFile)
    })
}

// RubyGemsUpload handles RubyGems package uploads
func (m *UploadManager) RubyGemsUpload(ctx context.Context, opts UploadOptions) error {
    // Find .gem file
    var gemFile string
    for _, file := range opts.Files {
        if filepath.Ext(file) == ".gem" {
            gemFile = file
            break
        }
    }

    if gemFile == "" {
        return fmt.Errorf("missing required .gem file")
    }

    // Parse and validate gem
    spec, err := parseGemspec(gemFile)
    if err != nil {
        return fmt.Errorf("failed to parse .gem: %w", err)
    }

    // Verify version matches
    if spec.Version != opts.Version {
        return fmt.Errorf("version mismatch: .gem has %s, expected %s",
            spec.Version, opts.Version)
    }

    // Check metadata size limit
    if err := validateGemMetadata(gemFile); err != nil {
        return err
    }

    // Upload gem
    return m.retryableUpload(ctx, func() error {
        return m.client.uploadRubyGem(opts.Organization, gemFile)
    })
}

// Generic helpers
func validateContainerUpload(opts UploadOptions) error {
    if opts.Organization == "" {
        return fmt.Errorf("organization is required")
    }
    if opts.PackageName == "" {
        return fmt.Errorf("package name is required")
    }
    if opts.Version == "" {
        return fmt.Errorf("version is required")
    }
    return nil
}

func isContainerLayer(file string) bool {
    ext := filepath.Ext(file)
    return ext == ".tar" || ext == ".gz" || ext == ".tgz"
}
