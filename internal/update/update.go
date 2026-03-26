// Package update checks GitHub for newer releases of claude-monitor.
package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// apiURL is the GitHub API endpoint. Overridden in tests.
var apiURL = "https://api.github.com/repos/Zxela/claude-monitor/releases/latest"

// Release describes an available update.
type Release struct {
	Version string // e.g. "v1.17.0"
	URL     string // GitHub release page URL
}

// ghRelease is the subset of the GitHub API response we need.
type ghRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// CheckLatest checks whether a newer version is available.
// Returns nil, nil if current version is up-to-date or is "dev".
func CheckLatest(currentVersion string) (*Release, error) {
	if currentVersion == "dev" {
		return nil, nil
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("update check: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update check: GitHub API returned %d", resp.StatusCode)
	}

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("update check: %w", err)
	}

	if compareVersions(currentVersion, release.TagName) < 0 {
		return &Release{
			Version: release.TagName,
			URL:     release.HTMLURL,
		}, nil
	}

	return nil, nil
}

// compareVersions compares two semver strings (with or without "v" prefix).
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareVersions(a, b string) int {
	aParts := parseSemver(a)
	bParts := parseSemver(b)

	for i := 0; i < 3; i++ {
		if aParts[i] < bParts[i] {
			return -1
		}
		if aParts[i] > bParts[i] {
			return 1
		}
	}
	return 0
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	var result [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		n, _ := strconv.Atoi(parts[i])
		result[i] = n
	}
	return result
}
