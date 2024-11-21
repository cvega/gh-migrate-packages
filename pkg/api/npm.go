package api

import (
    "encoding/json"
    "fmt"
    "io"
    "os"
    "path/filepath"
)

// NPM package.json structure
type NPMPackage struct {
    Name        string            `json:"name"`
    Version     string            `json:"version"`
    Description string            `json:"description,omitempty"`
    Author      interface{}       `json:"author,omitempty"`
    License     string            `json:"license,omitempty"`
    Repository  interface{}       `json:"repository,omitempty"`
    Dependencies map[string]string `json:"dependencies,omitempty"`
    Scripts     map[string]string `json:"scripts,omitempty"`
    Dist        struct {
        Shasum  string `json:"shasum"`
        Tarball string `json:"tarball"`
    } `json:"dist"`
}

func parseNPMPackage(packageJSON string) (*NPMPackage, error) {
    data, err := os.ReadFile(packageJSON)
    if err != nil {
        return nil, fmt.Errorf("failed to read package.json: %v", err)
    }

    var pkg NPMPackage
    if err := json.Unmarshal(data, &pkg); err != nil {
        return nil, fmt.Errorf("failed to parse package.json: %v", err)
    }

    // Validate required fields
    if pkg.Name == "" {
        return nil, fmt.Errorf("package.json missing required field: name")
    }
    if pkg.Version == "" {
        return nil, fmt.Errorf("package.json missing required field: version")
    }

    return &pkg, nil
}

func (a *API) publishNPMPackage(opts UploadOptions, packageJSON, tarball string) error {
    // Parse and validate package.json
    pkg, err := parseNPMPackage(packageJSON)
    if err != nil {
        return err
    }

    // Calculate integrity hash for tarball
    sha512, err := calculateFileHash(tarball, "sha512")
    if err != nil {
        return err
    }

    // Update package.json with GitHub-specific fields
    pkg.Repository = map[string]string{
        "type": "git",
        "url":  fmt.Sprintf("https://github.com/%s/%s.git", opts.Organization, filepath.Base(opts.PackageName)),
    }
    pkg.Dist.Shasum = sha512
    pkg.Dist.Tarball = fmt.Sprintf("https://npm.pkg.github.com/%s/-/%s-%s.tgz",
        opts.Organization, pkg.Name, pkg.Version)

    return nil
}
