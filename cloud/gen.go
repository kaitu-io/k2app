//go:build ignore

package main

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// webappDistURL is the download URL for the webapp distribution zip.
// When empty, the download is skipped and the existing dist/ contents are kept.
const webappDistURL = ""

func main() {
	url := webappDistURL
	if url == "" {
		fmt.Println("cloud/gen: WebappDistURL is empty, skipping download (keeping default placeholder)")
		return
	}

	distDir := "dist"
	zipPath := "dist.zip"

	// Download the zip file.
	fmt.Printf("cloud/gen: downloading %s\n", url)
	if err := download(url, zipPath); err != nil {
		fmt.Fprintf(os.Stderr, "cloud/gen: download failed: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(zipPath)

	// Clean dist/ directory before extraction (preserve directory itself).
	if err := cleanDir(distDir); err != nil {
		fmt.Fprintf(os.Stderr, "cloud/gen: clean dist/ failed: %v\n", err)
		os.Exit(1)
	}

	// Extract the zip into dist/.
	fmt.Printf("cloud/gen: extracting to %s/\n", distDir)
	if err := extractZip(zipPath, distDir); err != nil {
		fmt.Fprintf(os.Stderr, "cloud/gen: extract failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("cloud/gen: done")
}

// download fetches url and writes it to dst, printing progress.
func download(url, dst string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}

	f, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer f.Close()

	written, err := io.Copy(f, resp.Body)
	if err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}

	fmt.Printf("cloud/gen: downloaded %d bytes\n", written)
	return nil
}

// cleanDir removes all contents of dir but keeps the directory itself.
func cleanDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return os.MkdirAll(dir, 0o755)
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// extractZip extracts a zip file into dst directory.
func extractZip(zipPath, dst string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		target := filepath.Join(dst, f.Name)

		// Guard against zip slip (path traversal).
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dst)+string(os.PathSeparator)) {
			return fmt.Errorf("zip slip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}

		// Ensure parent directory exists.
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		if err := extractFile(f, target); err != nil {
			return err
		}
	}
	return nil
}

// extractFile writes a single zip entry to the filesystem.
func extractFile(f *zip.File, dst string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open %s in zip: %w", f.Name, err)
	}
	defer rc.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, rc); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}
