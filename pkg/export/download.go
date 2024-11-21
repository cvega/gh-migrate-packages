package api

import (
    "fmt"
    "io"
    "net/http"
    "os"
)

func (a *API) DownloadFile(url, destPath string) error {
    // Create a new HTTP request
    req, err := http.NewRequestWithContext(a.ctx, "GET", url, nil)
    if err != nil {
        return fmt.Errorf("failed to create request: %v", err)
    }

    // Send the request
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return fmt.Errorf("failed to download file: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("download failed with status: %s", resp.Status)
    }

    // Create the destination file
    file, err := os.Create(destPath)
    if err != nil {
        return fmt.Errorf("failed to create file: %v", err)
    }
    defer file.Close()

    // Copy the response body to the file
    _, err = io.Copy(file, resp.Body)
    if err != nil {
        return fmt.Errorf("failed to save file: %v", err)
    }

    return nil
}
