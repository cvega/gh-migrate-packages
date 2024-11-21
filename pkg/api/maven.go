package api

import (
    "bytes"
    "encoding/xml"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "strings"
)

// Maven POM structures
type MavenPOM struct {
    XMLName    xml.Name `xml:"project"`
    GroupID    string   `xml:"groupId"`
    ArtifactID string   `xml:"artifactId"`
    Version    string   `xml:"version"`
    Parent     *Parent  `xml:"parent"`
}

type Parent struct {
    GroupID    string `xml:"groupId"`
    ArtifactID string `xml:"artifactId"`
    Version    string `xml:"version"`
}

func parseMavenPOM(pomFile string) (groupID, artifactID string, err error) {
    file, err := os.Open(pomFile)
    if err != nil {
        return "", "", fmt.Errorf("failed to open POM file: %v", err)
    }
    defer file.Close()

    data, err := io.ReadAll(file)
    if err != nil {
        return "", "", fmt.Errorf("failed to read POM file: %v", err)
    }

    var pom MavenPOM
    if err := xml.Unmarshal(data, &pom); err != nil {
        return "", "", fmt.Errorf("failed to parse POM file: %v", err)
    }

    // Use parent group ID if project group ID is not set
    groupID = pom.GroupID
    if groupID == "" && pom.Parent != nil {
        groupID = pom.Parent.GroupID
    }

    if groupID == "" {
        return "", "", fmt.Errorf("no group ID found in POM file")
    }

    if pom.ArtifactID == "" {
        return "", "", fmt.Errorf("no artifact ID found in POM file")
    }

    return groupID, pom.ArtifactID, nil
}

func (a *API) uploadMavenFile(url, file string) error {
    // Calculate checksums
    data, err := os.ReadFile(file)
    if err != nil {
        return fmt.Errorf("failed to read file: %v", err)
    }

    md5sum := md5.Sum(data)
    sha1sum := sha1.Sum(data)
    sha256sum := sha256.Sum256(data)

    // Upload the main file
    if err := a.uploadFile(url, bytes.NewReader(data)); err != nil {
        return fmt.Errorf("failed to upload file: %v", err)
    }

    // Upload checksums
    checksums := map[string][]byte{
        url + ".md5":    []byte(fmt.Sprintf("%x", md5sum)),
        url + ".sha1":   []byte(fmt.Sprintf("%x", sha1sum)),
        url + ".sha256": []byte(fmt.Sprintf("%x", sha256sum)),
    }

    for checksumURL, checksumData := range checksums {
        if err := a.uploadFile(checksumURL, bytes.NewReader(checksumData)); err != nil {
            return fmt.Errorf("failed to upload checksum: %v", err)
        }
    }

    return nil
}

func (a *API) uploadFile(url string, content io.Reader) error {
    req, err := http.NewRequestWithContext(a.ctx, "PUT", url, content)
    if err != nil {
        return err
    }

    req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.token))

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
        return fmt.Errorf("upload failed with status: %s", resp.Status)
    }

    return nil
}

// Helper function to create Maven repository path
func createMavenPath(groupID, artifactID, version, filename string) string {
    // Convert group ID to path (org.example -> org/example)
    groupPath := strings.Replace(groupID, ".", "/", -1)
    
    // Create full path
    return filepath.Join(
        groupPath,
        artifactID,
        version,
        filename,
    )
}
