package release

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultAPIBaseURL  = "https://api.github.com"
	maxMetadataBytes   = 4 << 20
	maxChecksumBytes   = 1 << 20
	maxArchiveBytes    = 256 << 20
	maxExecutableBytes = 512 << 20
)

type Asset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
	Size        int64  `json:"size"`
}

type CheckResult struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Asset           Asset  `json:"asset"`
	ChecksumAsset   Asset  `json:"checksumAsset"`
}

type UpgradeResult struct {
	CheckResult
	Target string `json:"target"`
	SHA256 string `json:"sha256,omitempty"`
}

type Client struct {
	Repository string
	APIBaseURL string
	HTTPClient *http.Client
	GOOS       string
	GOARCH     string
}

type githubRelease struct {
	TagName    string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

func ArchiveName(version, goos, goarch string) string {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	extension := ".tar.gz"
	if goos == "windows" {
		extension = ".zip"
	}
	return fmt.Sprintf("superscience_%s_%s_%s%s", version, goos, goarch, extension)
}

func (c Client) Check(ctx context.Context, currentVersion string) (CheckResult, error) {
	repository := strings.TrimSpace(c.Repository)
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || !validRepositoryComponent(parts[0]) || !validRepositoryComponent(parts[1]) {
		return CheckResult{}, errors.New("release repository must use owner/name format")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(c.APIBaseURL), "/")
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}
	if err := validateReleaseURL(baseURL, "release API"); err != nil {
		return CheckResult{}, err
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/releases/latest", baseURL, parts[0], parts[1])
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return CheckResult{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "superscience-updater")
	response, err := c.httpClient().Do(request)
	if err != nil {
		return CheckResult{}, fmt.Errorf("query latest release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return CheckResult{}, fmt.Errorf("query latest release: HTTP %d", response.StatusCode)
	}
	var published githubRelease
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxMetadataBytes+1))
	if err := decoder.Decode(&published); err != nil {
		return CheckResult{}, fmt.Errorf("decode latest release: %w", err)
	}
	if published.Draft || published.TagName == "" {
		return CheckResult{}, errors.New("latest release metadata is not publishable")
	}
	if published.Prerelease {
		return CheckResult{}, errors.New("latest release metadata points to a prerelease")
	}
	if _, err := parseVersion(published.TagName); err != nil {
		return CheckResult{}, fmt.Errorf("latest release tag: %w", err)
	}
	goos, goarch := c.platform()
	archiveName := ArchiveName(published.TagName, goos, goarch)
	var archive Asset
	var checksums Asset
	for _, asset := range published.Assets {
		switch asset.Name {
		case archiveName:
			archive = asset
		case "checksums.txt":
			checksums = asset
		}
	}
	if archive.Name == "" || archive.DownloadURL == "" {
		return CheckResult{}, fmt.Errorf("release %s does not include %s", published.TagName, archiveName)
	}
	if checksums.Name == "" || checksums.DownloadURL == "" {
		return CheckResult{}, fmt.Errorf("release %s does not include checksums.txt", published.TagName)
	}
	if err := validateReleaseURL(archive.DownloadURL, "release archive"); err != nil {
		return CheckResult{}, err
	}
	if err := validateReleaseURL(checksums.DownloadURL, "release checksum manifest"); err != nil {
		return CheckResult{}, err
	}
	available, err := updateAvailable(currentVersion, published.TagName)
	if err != nil {
		return CheckResult{}, err
	}
	return CheckResult{
		CurrentVersion:  strings.TrimSpace(currentVersion),
		LatestVersion:   published.TagName,
		UpdateAvailable: available,
		Asset:           archive,
		ChecksumAsset:   checksums,
	}, nil
}

func (c Client) Upgrade(ctx context.Context, currentVersion, target string) (UpgradeResult, error) {
	check, err := c.Check(ctx, currentVersion)
	if err != nil {
		return UpgradeResult{}, err
	}
	result := UpgradeResult{CheckResult: check, Target: target}
	if !check.UpdateAvailable {
		return result, nil
	}
	if err := validateInstallTarget(target); err != nil {
		return UpgradeResult{}, err
	}
	checksums, err := c.download(ctx, check.ChecksumAsset, maxChecksumBytes)
	if err != nil {
		return UpgradeResult{}, fmt.Errorf("download checksums: %w", err)
	}
	expected, err := checksumFor(checksums, check.Asset.Name)
	if err != nil {
		return UpgradeResult{}, err
	}
	archive, err := c.download(ctx, check.Asset, maxArchiveBytes)
	if err != nil {
		return UpgradeResult{}, fmt.Errorf("download release archive: %w", err)
	}
	actual := sha256.Sum256(archive)
	if subtle.ConstantTimeCompare(expected, actual[:]) != 1 {
		return UpgradeResult{}, fmt.Errorf("release archive checksum mismatch for %s", check.Asset.Name)
	}
	executable, mode, err := extractExecutable(archive, c.platformOS())
	if err != nil {
		return UpgradeResult{}, err
	}
	if err := atomicInstall(target, executable, mode); err != nil {
		return UpgradeResult{}, err
	}
	result.SHA256 = hex.EncodeToString(actual[:])
	return result, nil
}

func (c Client) platform() (string, string) {
	goos := strings.TrimSpace(c.GOOS)
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := strings.TrimSpace(c.GOARCH)
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	return goos, goarch
}

func (c Client) platformOS() string {
	goos, _ := c.platform()
	return goos
}

func (c Client) httpClient() *http.Client {
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	clone := *client
	previousRedirect := clone.CheckRedirect
	clone.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("release download exceeded the redirect limit")
		}
		if err := validateReleaseURL(request.URL.String(), "release redirect"); err != nil {
			return err
		}
		if previousRedirect != nil {
			return previousRedirect(request, via)
		}
		return nil
	}
	return &clone
}

func (c Client) download(ctx context.Context, asset Asset, limit int64) ([]byte, error) {
	if asset.Size < 0 || asset.Size > limit {
		return nil, fmt.Errorf("asset %s exceeds the %d byte limit", asset.Name, limit)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.DownloadURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "superscience-updater")
	response, err := c.httpClient().Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("asset %s returned HTTP %d", asset.Name, response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("asset %s exceeds the %d byte limit", asset.Name, limit)
	}
	return payload, nil
}

func checksumFor(payload []byte, assetName string) ([]byte, error) {
	var selected []byte
	for _, line := range strings.Split(string(payload), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.TrimPrefix(fields[1], "*") != assetName {
			continue
		}
		decoded, err := hex.DecodeString(fields[0])
		if err != nil || len(decoded) != sha256.Size {
			return nil, fmt.Errorf("invalid SHA-256 checksum for %s", assetName)
		}
		if selected != nil && subtle.ConstantTimeCompare(selected, decoded) != 1 {
			return nil, fmt.Errorf("conflicting checksums for %s", assetName)
		}
		selected = decoded
	}
	if selected == nil {
		return nil, fmt.Errorf("checksums.txt does not contain %s", assetName)
	}
	return selected, nil
}

func validateInstallTarget(target string) error {
	if strings.TrimSpace(target) == "" {
		return errors.New("upgrade target is required")
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if !recognizedExecutableName(filepath.Base(absolute)) {
		return fmt.Errorf("upgrade target is not a Superscience executable: %s", absolute)
	}
	if info, err := os.Lstat(absolute); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("upgrade target must be a regular file: %s", absolute)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func validRepositoryComponent(value string) bool {
	if value == "" || value == "." || value == ".." || len(value) > 100 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '-' && character != '_' && character != '.' {
			return false
		}
	}
	return true
}

func validateReleaseURL(rawURL, label string) error {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("%s URL is invalid", label)
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme == "http" && loopbackHost(parsed.Hostname()) {
		return nil
	}
	return fmt.Errorf("%s URL must use HTTPS unless it targets loopback", label)
}

func loopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	address := net.ParseIP(strings.TrimSpace(host))
	return address != nil && address.IsLoopback()
}
