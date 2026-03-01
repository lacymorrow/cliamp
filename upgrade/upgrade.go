// Package upgrade implements self-upgrade by downloading the latest
// release binary from GitHub, mirroring the install.sh mechanism.
package upgrade

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const repo = "bjarneo/cliamp"

var httpClient = &http.Client{Timeout: 30 * time.Second}

type release struct {
	TagName string `json:"tag_name"`
}

// Run checks for a newer release and replaces the current binary if one is found.
func Run(currentVersion string) error {
	latest, err := latestVersion()
	if err != nil {
		return fmt.Errorf("checking latest version: %w", err)
	}

	if currentVersion != "" && currentVersion == latest {
		fmt.Printf("Already up to date (%s)\n", currentVersion)
		return nil
	}

	if currentVersion == "" {
		fmt.Printf("Latest release is %s, downloading...\n", latest)
	} else {
		fmt.Printf("Upgrading %s → %s\n", currentVersion, latest)
	}

	binaryName := fmt.Sprintf("cliamp-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}

	url := fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", repo, binaryName)

	fmt.Printf("Downloading %s...\n", binaryName)

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating current binary: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolving binary path: %w", err)
	}

	if err := downloadAndReplace(url, exe); err != nil {
		return err
	}

	fmt.Printf("Upgraded to %s\n", latest)
	return nil
}

func latestVersion() (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	resp, err := httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var r release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}
	return r.TagName, nil
}

func downloadAndReplace(url, destPath string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	// Write to a temp file in the same directory as the target so
	// os.Rename works (same filesystem).
	dir := filepath.Dir(destPath)
	tmp, err := os.CreateTemp(dir, "cliamp-upgrade-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w (try running with sudo)", err)
	}
	tmpPath := tmp.Name()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("writing binary: %w", err)
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("setting permissions: %w", err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("replacing binary: %w (try running with sudo)", err)
	}

	return nil
}
