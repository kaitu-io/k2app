// Copyright 2024 Kaitu. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build ignore

// gen.go downloads and extracts wintun.dll for embedding.
// Run via: go generate ./wintun/
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const (
	wintunURL    = "https://www.wintun.net/builds/wintun-0.14.1.zip"
	wintunSHA256 = "07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51"
)

// Architectures to extract: zip path → local filename.
var archDLLs = map[string]string{
	"wintun/bin/amd64/wintun.dll": "wintun_amd64.dll",
	"wintun/bin/arm64/wintun.dll": "wintun_arm64.dll",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gen: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Download the zip.
	fmt.Printf("downloading %s\n", wintunURL)
	resp, err := http.Get(wintunURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	zipFile := "wintun.zip"
	f, err := os.Create(zipFile)
	if err != nil {
		return fmt.Errorf("create zip: %w", err)
	}

	h := sha256.New()
	if _, err := io.Copy(f, io.TeeReader(resp.Body, h)); err != nil {
		f.Close()
		os.Remove(zipFile)
		return fmt.Errorf("download: %w", err)
	}
	f.Close()

	// Verify SHA-256.
	got := hex.EncodeToString(h.Sum(nil))
	if got != wintunSHA256 {
		os.Remove(zipFile)
		return fmt.Errorf("sha256 mismatch: got %s, want %s", got, wintunSHA256)
	}
	fmt.Println("sha256 verified")

	// Extract architecture-specific DLLs from the zip.
	for zipPath, localName := range archDLLs {
		if err := extractDLL(zipFile, zipPath, localName); err != nil {
			os.Remove(zipFile)
			return err
		}
		fmt.Printf("extracted: %s\n", localName)
	}

	// Clean up the zip.
	os.Remove(zipFile)
	fmt.Println("done")
	return nil
}

func extractDLL(zipPath, entryPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if filepath.ToSlash(f.Name) != entryPath {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open entry: %w", err)
		}
		defer rc.Close()

		out, err := os.Create(dest)
		if err != nil {
			return fmt.Errorf("create %s: %w", dest, err)
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			return fmt.Errorf("extract: %w", err)
		}
		return out.Close()
	}
	return fmt.Errorf("entry %s not found in zip", entryPath)
}
