package export

import (
    "encoding/csv"
    "fmt"
    "os"
    "path/filepath"
    "strconv"
    "sync"
    "time"

    "github.com/pterm/pterm"
    "github.com/spf13/viper"
    "github.com/cvega/gh-migrate-packages/pkg/api"
)

type ExportOptions struct {
    DownloadPath string
    FilePrefix   string
    Organization string
    PackageType  string
}

type ExportResult struct {
    PackagesExported   int
    VersionsExported   int
    DownloadsComplete  int
    DownloadsFailed    int
    TotalSizeDownloaded int64
}

func CreateCSVs() (*ExportResult, error) {
    opt := ExportOptions{
        DownloadPath: viper.GetString("DOWNLOAD_PATH"),
        FilePrefix:   viper.GetString("OUTPUT_FILE"),
        Organization: viper.GetString("SOURCE_ORGANIZATION"),
        PackageType:  viper.GetString("PACKAGE_TYPE"),
    }

    if opt.DownloadPath == "" {
        opt.DownloadPath = "downloads"
    }

    if opt.FilePrefix == "" {
        opt.FilePrefix = opt.Organization
    }

    // Initialize API client
    apiClient := api.NewAPI(
        viper.GetString("SOURCE_TOKEN"),
        viper.GetString("SOURCE_HOSTNAME"),
    )

    // Create results struct
    result := &ExportResult{}

    // Create export directory
    if err := os.MkdirAll(opt.DownloadPath, 0755); err != nil {
        return nil, fmt.Errorf("failed to create download directory: %v", err)
    }

    // Initialize progress tracking
    packagesSpinner, _ := pterm.DefaultSpinner.Start("Fetching packages...")

    // Fetch packages
    packages, err := apiClient.GetOrganizationPackages(opt.Organization, opt.PackageType)
    if err != nil {
        packagesSpinner.Fail(err.Error())
        return nil, err
    }

    packagesSpinner.Success(fmt.Sprintf("Found %d packages", len(packages)))

    // Create CSV files
    if err := createPackagesCSV(opt.FilePrefix, packages); err != nil {
        return nil, fmt.Errorf("failed to create packages CSV: %v", err)
    }
    result.PackagesExported = len(packages)

    if err := createVersionsCSV(opt.FilePrefix, packages); err != nil {
        return nil, fmt.Errorf("failed to create versions CSV: %v", err)
    }

    // Count total versions
    totalVersions := 0
    for _, pkg := range packages {
        totalVersions += len(pkg.Versions)
    }
    result.VersionsExported = totalVersions

    // Download packages if path is specified
    if opt.DownloadPath != "" {
        downloadResults := downloadPackages(apiClient, packages, opt.DownloadPath)
        result.DownloadsComplete = downloadResults.complete
        result.DownloadsFailed = downloadResults.failed
        result.TotalSizeDownloaded = downloadResults.totalSize
    }

    return result, nil
}

func createPackagesCSV(prefix string, packages []api.Package) error {
    filename := fmt.Sprintf("%s_packages.csv", prefix)
    file, err := os.Create(filename)
    if err != nil {
        return err
    }
    defer file.Close()

    writer := csv.NewWriter(file)
    defer writer.Flush()

    // Write header
    header := []string{
        "ID", "Name", "Type", "Repository", "Repository URL",
        "Downloads Count", "Version Count",
    }
    if err := writer.Write(header); err != nil {
        return err
    }

    // Write package data
    for _, pkg := range packages {
        row := []string{
            pkg.ID,
            pkg.Name,
            pkg.PackageType,
            pkg.Repository.Name,
            pkg.Repository.URL,
            strconv.Itoa(pkg.Statistics.DownloadsCount),
            strconv.Itoa(len(pkg.Versions)),
        }
        if err := writer.Write(row); err != nil {
            return err
        }
    }

    return nil
}

func createVersionsCSV(prefix string, packages []api.Package) error {
    filename := fmt.Sprintf("%s_versions.csv", prefix)
    file, err := os.Create(filename)
    if err != nil {
        return err
    }
    defer file.Close()

    writer := csv.NewWriter(file)
    defer writer.Flush()

    // Write header
    header := []string{
        "Package ID", "Package Name", "Version ID", "Version",
        "Created At", "Updated At", "File Count", "Total Size",
    }
    if err := writer.Write(header); err != nil {
        return err
    }

    // Write version data
    for _, pkg := range packages {
        for _, ver := range pkg.Versions {
            var totalSize int
            for _, file := range ver.Files {
                totalSize += file.Size
            }

            row := []string{
                pkg.ID,
                pkg.Name,
                ver.ID,
                ver.Name,
                ver.CreatedAt,
                ver.UpdatedAt,
                strconv.Itoa(len(ver.Files)),
                strconv.Itoa(totalSize),
            }
            if err := writer.Write(row); err != nil {
                return err
            }
        }
    }

    return nil
}

type downloadResult struct {
    complete  int
    failed    int
    totalSize int64
}

func downloadPackages(client *api.API, packages []api.Package, downloadPath string) downloadResult {
    result := downloadResult{}
    var wg sync.WaitGroup
    semaphore := make(chan struct{}, 5) // Limit concurrent downloads
    
    // Create progress bar
    progressbar, _ := pterm.DefaultProgressbar.
        WithTotal(getTotalVersions(packages)).
        WithTitle("Downloading package versions").
        Start()

    for _, pkg := range packages {
        // Create package directory
        pkgDir := filepath.Join(downloadPath, pkg.PackageType, pkg.Name)
        if err := os.MkdirAll(pkgDir, 0755); err != nil {
            pterm.Error.Printf("Failed to create directory for package %s: %v\n", pkg.Name, err)
            continue
        }

        for _, version := range pkg.Versions {
            wg.Add(1)
            semaphore <- struct{}{} // Acquire semaphore

            go func(p api.Package, v api.Version, dir string) {
                defer wg.Done()
                defer func() { <-semaphore }() // Release semaphore

                versionDir := filepath.Join(dir, v.Name)
                if err := os.MkdirAll(versionDir, 0755); err != nil {
                    pterm.Error.Printf("Failed to create directory for version %s: %v\n", v.Name, err)
                    result.failed++
                    return
                }

                // Create metadata file
                metadataFile := filepath.Join(versionDir, "metadata.json")
                if err := createMetadataFile(metadataFile, p, v); err != nil {
                    pterm.Error.Printf("Failed to create metadata for version %s: %v\n", v.Name, err)
                }

                // Download each file
                for _, file := range v.Files {
                    filePath := filepath.Join(versionDir, file.Name)
                    
                    // Skip if file already exists with correct size
                    if fileExists(filePath, file.Size) {
                        progressbar.Increment()
                        continue
                    }

                    if err := downloadFile(client, file.URL, filePath); err != nil {
                        pterm.Error.Printf("Failed to download %s: %v\n", file.Name, err)
                        result.failed++
                    } else {
                        result.complete++
                        result.totalSize += int64(file.Size)
                    }
                }

                progressbar.Increment()
            }(pkg, version, pkgDir)
        }
    }

    wg.Wait()
    progressbar.Stop()

    return result
}

func getTotalVersions(packages []api.Package) int {
    total := 0
    for _, pkg := range packages {
        total += len(pkg.Versions)
    }
    return total
}

func createMetadataFile(path string, pkg api.Package, version api.Version) error {
    metadata := map[string]interface{}{
        "package": map[string]interface{}{
            "id":          pkg.ID,
            "name":        pkg.Name,
            "type":        pkg.PackageType,
            "repository":  pkg.Repository,
            "statistics": pkg.Statistics,
        },
        "version": map[string]interface{}{
            "id":         version.ID,
            "name":       version.Name,
            "created_at": version.CreatedAt,
            "updated_at": version.UpdatedAt,
            "files":      version.Files,
        },
        "exported_at": time.Now().UTC(),
    }

    file, err := os.Create(path)
    if err != nil {
        return err
    }
    defer file.Close()

    encoder := json.NewEncoder(file)
    encoder.SetIndent("", "  ")
    return encoder.Encode(metadata)
}

func fileExists(path string, expectedSize int) bool {
    info, err := os.Stat(path)
    if err != nil {
        return false
    }
    return info.Size() == int64(expectedSize)
}

func downloadFile(client *api.API, url, path string) error {
    // Implementation to be added in API client
    // This should handle the actual file download using the client
    return client.DownloadFile(url, path)
}
