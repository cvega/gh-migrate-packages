package api

import (
    "archive/zip"
    "encoding/xml"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"
)

// NuGet .nuspec structure
type NuspecManifest struct {
    XMLName     xml.Name `xml:"package"`
    Metadata    Metadata `xml:"metadata"`
}

type Metadata struct {
    ID          string `xml:"id"`
    Version     string `xml:"version"`
    Description string `xml:"description,omitempty"`
    Authors     string `xml:"authors,omitempty"`
    Repository  struct {
        Type string `xml:"type,attr"`
        URL  string `xml:"url,attr"`
    } `xml:"repository"`
    Dependencies Dependencies `xml:"dependencies"`
}

type Dependencies struct {
    Groups []DependencyGroup `xml:"group"`
}

type DependencyGroup struct {
    TargetFramework string       `xml:"targetFramework,attr"`
    Dependencies    []Dependency `xml:"dependency"`
}

type Dependency struct {
    ID      string `xml:"id,attr"`
    Version string `xml:"version,attr"`
}

func parseNuspec(nupkgPath string) (*NuspecManifest, error) {
    // Open the .nupkg file (it's actually a ZIP file)
    reader, err := zip.OpenReader(nupkgPath)
    if err != nil {
        return nil, fmt.Errorf("failed to open nupkg: %v", err)
    }
    defer reader.Close()

    // Find and read the .nuspec file
    var nuspecFile *zip.File
    for _, file := range reader.File {
        if strings.HasSuffix(file.Name, ".nuspec") {
            nuspecFile = file
            break
        }
    }

    if nuspecFile == nil {
        return nil, fmt.Errorf("no .nuspec file found in package")
    }

    // Read the .nuspec content
    rc, err := nuspecFile.Open()
    if err != nil {
        return nil, fmt.Errorf("failed to open nuspec: %v", err)
    }
    defer rc.Close()

    var manifest NuspecManifest
    if err := xml.NewDecoder(rc).Decode(&manifest); err != nil {
        return nil, fmt.Errorf("failed to parse nuspec: %v", err)
    }

    return &manifest, nil
}

func (a *API) publishNuGetPackage(opts UploadOptions, nupkgPath string) error {
    // Parse and validate .nupkg
    manifest, err := parseNuspec(nupkgPath)
    if err != nil {
        return err
    }

    // Validate required fields
    if manifest.Metadata.ID == "" {
        return fmt.Errorf("nuspec missing required field: id")
    }
    if manifest.Metadata.Version == "" {
        return fmt.Errorf("nuspec missing required field: version")
    }

    // Update repository information
    manifest.Metadata.Repository.Type = "git"
    manifest.Metadata.Repository.URL = fmt.Sprintf("https://github.com/%s/%s",
        opts.Organization, manifest.Metadata.ID)

    return nil
}
