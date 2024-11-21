package sync

import (
    "fmt"
    "log"
    "path/filepath"
    "strings"
    "encoding/csv"
    "os"

    "github.com/pterm/pterm"
    "github.com/spf13/viper"
    "github.com/cvega/gh-migrate-packages/pkg/api"
    "github.com/cvega/gh-migrate-packages/pkg/package"
)

type PackageSync struct {
    sourceAPI *api.API
    targetAPI *api.API
    mappings  map[string]string // For package name mappings if provided
}

type ValidationReport struct {
    PackageName    string
    PackageType    string
    Status         string
    ErrorMessage   string
    VersionsCount  int
    TotalSize     int64
}

func NewPackageSync(sourceToken, targetToken, sourceHost string) *PackageSync {
    return &PackageSync{
        sourceAPI: api.NewAPI(sourceToken, sourceHost),
        targetAPI: api.NewAPI(targetToken, ""),
        mappings:  make(map[string]string),
    }
}

func (s *PackageSync) LoadMappings(mappingFile string) error {
    if mappingFile == "" {
        return nil // No mappings to load
    }

    file, err := os.Open(mappingFile)
    if err != nil {
        return fmt.Errorf("failed to open mapping file: %v", err)
    }
    defer file.Close()

    reader := csv.NewReader(file)
    records, err := reader.ReadAll()
    if err != nil {
        return fmt.Errorf("failed to read mapping file: %v", err)
    }

    for _, record := range records[1:] { // Skip header row
        if len(record) >= 2 {
            s.mappings[record[0]] = record[1]
        }
    }

    return nil
}

func (s *PackageSync) getTargetPackageName(sourceName string) string {
    if targetName, exists := s.mappings[sourceName]; exists {
        return targetName
    }
    return sourceName
}

func (s *PackageSync) processConcurrently(packages []pkg.Package) {
    const maxConcurrent = 5
    sem := make(chan bool, maxConcurrent)
    results := make(chan *ValidationReport)
    var wg sync.WaitGroup

    // Start progress tracking
    progressbar, _ := pterm.DefaultProgressbar.
        WithTotal(len(packages)).
        WithTitle("Migrating packages").
        Start()

    // Start validation reporter
    go s.reportValidation(results)

    for _, p := range packages {
        wg.Add(1)
        sem <- true // Acquire semaphore
        
        go func(pkg pkg.Package) {
            defer func() {
                <-sem // Release semaphore
                wg.Done()
            }()

            report := &ValidationReport{
                PackageName:   pkg.Name,
                PackageType:   pkg.PackageType,
                VersionsCount: len(pkg.Versions),
            }

            // Validate package
            if err := pkg.ValidatePackage(&pkg); err != nil {
                report.Status = "Failed"
                report.ErrorMessage = err.Error()
                results <- report
                return
            }

            targetName := s.getTargetPackageName(pkg.Name)
            
            // Process each version concurrently
            var versionWg sync.WaitGroup
            versionErrors := make(chan error, len(pkg.Versions))

            for _, version := range pkg.Versions {
                versionWg.Add(1)
                go func(v pkg.Version) {
                    defer versionWg.Done()

                    // Download package files with retry logic
                    var data []byte
                    var err error
                    for retries := 0; retries < maxRetries; retries++ {
                        data, err = s.sourceAPI.DownloadPackageVersion(
                            viper.GetString("SOURCE_ORGANIZATION"),
                            pkg.Name,
                            v.Name,
                        )
                        if err == nil {
                            break
                        }
                        time.Sleep(retryDelay * time.Duration(retries+1))
                    }
                    if err != nil {
                        versionErrors <- fmt.Errorf("failed to download version %s: %v", v.Name, err)
                        return
                    }

                    // Upload to target
                    err = s.targetAPI.UploadPackageVersion(
                        viper.GetString("TARGET_ORGANIZATION"),
                        targetName,
                        v.Name,
                        data,
                    )
                    if err != nil {
                        versionErrors <- fmt.Errorf("failed to upload version %s: %v", v.Name, err)
                        return
                    }
                }(version)
            }

            // Wait for all versions to complete
            versionWg.Wait()
            close(versionErrors)

            // Check for any version errors
            var errMsgs []string
            for err := range versionErrors {
                errMsgs = append(errMsgs, err.Error())
            }

            if len(errMsgs) > 0 {
                report.Status = "Partial Success"
                report.ErrorMessage = strings.Join(errMsgs, "; ")
            } else {
                report.Status = "Success"
            }

            results <- report
            progressbar.Increment()
        }(p)
    }

    // Wait for all packages to complete
    wg.Wait()
    close(results)
    progressbar.Stop()
}

func (s *PackageSync) reportValidation(results chan *ValidationReport) {
    successCount := 0
    failureCount := 0
    partialCount := 0
    
    table := pterm.TableData{
        {"Package", "Type", "Status", "Versions", "Error"},
    }

    for report := range results {
        status := report.Status
        switch status {
        case "Success":
            successCount++
            status = pterm.Green(status)
        case "Failed":
            failureCount++
            status = pterm.Red(status)
        case "Partial Success":
            partialCount++
            status = pterm.Yellow(status)
        }

        table = append(table, []string{
            report.PackageName,
            report.PackageType,
            status,
            fmt.Sprintf("%d", report.VersionsCount),
            report.ErrorMessage,
        })
    }

    pterm.DefaultTable.WithHasHeader().WithData(table).Render()

    pterm.Info.Printf("Migration Summary:\n")
    pterm.Info.Printf("- Successful: %d\n", successCount)
    pterm.Info.Printf("- Partial Success: %d\n", partialCount)
    pterm.Info.Printf("- Failed: %d\n", failureCount)
}

func SyncPackages() {
    spinner, _ := pterm.DefaultSpinner.Start("Initializing package synchronization...")

    // Initialize sync client
    sync := NewPackageSync(
        viper.GetString("SOURCE_TOKEN"),
        viper.GetString("TARGET_TOKEN"),
        viper.GetString("SOURCE_HOSTNAME"),
    )

    // Load mappings if provided
    if mappingFile := viper.GetString("MAPPING_FILE"); mappingFile != "" {
        spinner.UpdateText("Loading package name mappings...")
        if err := sync.LoadMappings(mappingFile); err != nil {
            spinner.Fail(fmt.Sprintf("Failed to load mappings: %v", err))
            return
        }
    }

    sourceOrg := viper.GetString("SOURCE_ORGANIZATION")
    targetOrg := viper.GetString("TARGET_ORGANIZATION")
    packageType := viper.GetString("PACKAGE_TYPE")
    skipExisting := viper.GetBool("SKIP_EXISTING")

    // Fetch source packages
    spinner.UpdateText("Fetching packages from source organization...")
    packages, err := sync.sourceAPI.GetOrganizationPackages(sourceOrg, packageType)
    if err != nil {
        spinner.Fail(fmt.Sprintf("Failed to fetch source packages: %v", err))
        return
    }

    spinner.Success("Package list retrieved successfully")

    // Process each package
    progressbar, _ := pterm.DefaultProgressbar.WithTotal(len(packages)).WithTitle("Migrating packages").Start()
    
    for _, pkg := range packages {
        progressbar.UpdateTitle(fmt.Sprintf("Processing %s", pkg.Name))

        // Validate package
        if err := pkg.ValidatePackage(&pkg); err != nil {
            log.Printf("Warning: Package %s validation failed: %v", pkg.Name, err)
            continue
        }

        // Check if package exists in target
        targetName := sync.getTargetPackageName(pkg.Name)
        exists, err := sync.targetAPI.PackageExists(targetOrg, targetName)
        if err != nil {
            log.Printf("Error checking package %s existence: %v", targetName, err)
            continue
        }

        if exists && skipExisting {
            log.Printf("Skipping existing package: %s", targetName)
            progressbar.Increment()
            continue
        }

        // Migrate each version
        for _, version := range pkg.Versions {
            spinner.UpdateText(fmt.Sprintf("Migrating %s version %s", pkg.Name, version.Name))

            // Download package files
            files, err := sync.sourceAPI.DownloadPackageVersion(sourceOrg, pkg.Name, version.Name)
            if err != nil {
                log.Printf("Error downloading version %s of package %s: %v", version.Name, pkg.Name, err)
                continue
            }

            // Upload to target
            err = sync.targetAPI.UploadPackageVersion(targetOrg, targetName, version.Name, files)
            if err != nil {
                log.Printf("Error uploading version %s of package %s: %v", version.Name, targetName, err)
                continue
            }

            // Copy package metadata
            err = sync.targetAPI.UpdatePackageMetadata(targetOrg, targetName, version.Name, version.Metadata)
            if err != nil {
                log.Printf("Error updating metadata for %s version %s: %v", targetName, version.Name, err)
            }
        }

        // Update visibility and permissions
        err = sync.targetAPI.UpdatePackageVisibility(targetOrg, targetName, pkg.Visibility)
        if err != nil {
            log.Printf("Error updating visibility for package %s: %v", targetName, err)
        }

        progressbar.Increment()
    }

    progressbar.Stop()
    spinner.Success("Package migration completed")
}
