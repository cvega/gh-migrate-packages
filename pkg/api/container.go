package api

import (
    "bytes"
    "crypto/sha256"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "strings"
)

// ContainerManifest represents an OCI compliant container manifest
type ContainerManifest struct {
    SchemaVersion int           `json:"schemaVersion"`
    MediaType     string        `json:"mediaType"`
    Config        ConfigObject  `json:"config"`
    Layers        []LayerObject `json:"layers"`
    Annotations   map[string]string `json:"annotations,omitempty"`
}

type ConfigObject struct {
    MediaType string `json:"mediaType"`
    Size      int64  `json:"size"`
    Digest    string `json:"digest"`
}

type LayerObject struct {
    MediaType string `json:"mediaType"`
    Size      int64  `json:"size"`
    Digest    string `json:"digest"`
}

const (
    mediaTypeManifest = "application/vnd.docker.distribution.manifest.v2+json"
    mediaTypeLayer    = "application/vnd.docker.image.rootfs.diff.tar.gzip"
    mediaTypeConfig   = "application/vnd.docker.container.image.v1+json"
)

func (a *API) uploadContainerLayer(baseURL, file string) (string, error) {
    // Calculate file size and SHA256
    fileInfo, err := os.Stat(file)
    if err != nil {
        return "", fmt.Errorf("failed to stat file: %v", err)
    }

    f, err := os.Open(file)
    if err != nil {
        return "", fmt.Errorf("failed to open file: %v", err)
    }
    defer f.Close()

    hash := sha256.New()
    if _, err := io.Copy(hash, f); err != nil {
        return "", fmt.Errorf("failed to calculate hash: %v", err)
    }
    digest := fmt.Sprintf("sha256:%x", hash.Sum(nil))

    // Check if layer already exists
    checkURL := fmt.Sprintf("%s/blobs/%s", baseURL, digest)
    exists, err := a.checkExists(checkURL)
    if err != nil {
        return "", err
    }
    if exists {
        return digest, nil // Layer already exists
    }

    // Start upload session
    uploadURL := fmt.Sprintf("%s/blobs/uploads/", baseURL)
    resp, err := a.post(uploadURL, nil)
    if err != nil {
        return "", fmt.Errorf("failed to start upload: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusAccepted {
        return "", fmt.Errorf("unexpected status starting upload: %s", resp.Status)
    }

    // Get upload location from header
    location := resp.Header.Get("Location")
    if location == "" {
        return "", fmt.Errorf("no upload location received")
    }

    // Upload the layer
    f.Seek(0, 0) // Reset file pointer to beginning
    uploadURL = fmt.Sprintf("%s&digest=%s", location, digest)
    
    req, err := http.NewRequestWithContext(a.ctx, "PUT", uploadURL, f)
    if err != nil {
        return "", err
    }

    req.Header.Set("Content-Type", mediaTypeLayer)
    req.Header.Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))
    req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.token))

    resp, err = http.DefaultClient.Do(req)
    if err != nil {
        return "", fmt.Errorf("failed to upload layer: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusCreated {
        return "", fmt.Errorf("layer upload failed with status: %s", resp.Status)
    }

    return digest, nil
}

func generateContainerManifest(layers []string, opts UploadOptions) (*ContainerManifest, error) {
    // Create config object first
    config := map[string]interface{}{
        "architecture": "amd64", // Default to amd64, could be made configurable
        "os": "linux",          // Default to linux, could be made configurable
        "rootfs": map[string]interface{}{
            "type":    "layers",
            "diff_ids": layers,
        },
        "history": []map[string]interface{}{
            {
                "created": opts.Metadata["created"],
                "comment": fmt.Sprintf("Imported via GitHub Packages Migration Tool"),
            },
        },
    }

    // Serialize config to get its hash
    configJson, err := json.Marshal(config)
    if err != nil {
        return nil, fmt.Errorf("failed to marshal config: %v", err)
    }

    configHash := sha256.Sum256(configJson)
    configDigest := fmt.Sprintf("sha256:%x", configHash)

    // Build the manifest
    manifest := &ContainerManifest{
        SchemaVersion: 2,
        MediaType:     mediaTypeManifest,
        Config: ConfigObject{
            MediaType: mediaTypeConfig,
            Size:      int64(len(configJson)),
            Digest:    configDigest,
        },
        Layers: make([]LayerObject, len(layers)),
    }

    // Add layers
    for i, digest := range layers {
        manifest.Layers[i] = LayerObject{
            MediaType: mediaTypeLayer,
            Size:     0, // Size will be set during upload
            Digest:   digest,
        }
    }

    // Add annotations if provided
    if len(opts.Metadata) > 0 {
        manifest.Annotations = make(map[string]string)
        for k, v := range opts.Metadata {
            if str, ok := v.(string); ok {
                manifest.Annotations[k] = str
            }
        }
    }

    return manifest, nil
}

func (a *API) uploadContainerManifest(url string, manifest *ContainerManifest) error {
    data, err := json.Marshal(manifest)
    if err != nil {
        return fmt.Errorf("failed to marshal manifest: %v", err)
    }

    req, err := http.NewRequestWithContext(a.ctx, "PUT", url, bytes.NewReader(data))
    if err != nil {
        return err
    }

    req.Header.Set("Content-Type", mediaTypeManifest)
    req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.token))

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return fmt.Errorf("failed to upload manifest: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusCreated {
        return fmt.Errorf("manifest upload failed with status: %s", resp.Status)
    }

    return nil
}

