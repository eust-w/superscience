package release

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type PackageOptions struct {
	Version    string
	GOOS       string
	GOARCH     string
	BinaryPath string
	OutputDir  string
}

type PackageResult struct {
	Version      string `json:"version"`
	GOOS         string `json:"goos"`
	GOARCH       string `json:"goarch"`
	ArchivePath  string `json:"archivePath"`
	ChecksumPath string `json:"checksumPath"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
}

func PackageBinary(options PackageOptions) (PackageResult, error) {
	version := strings.TrimSpace(options.Version)
	if _, err := parseVersion(version); err != nil {
		return PackageResult{}, err
	}
	goos := strings.TrimSpace(options.GOOS)
	goarch := strings.TrimSpace(options.GOARCH)
	if !validReleaseCoordinate(goos) || !validReleaseCoordinate(goarch) {
		return PackageResult{}, errors.New("release GOOS and GOARCH must contain only lowercase letters, numbers, or underscores")
	}
	if strings.TrimSpace(options.BinaryPath) == "" {
		return PackageResult{}, errors.New("release binary path is required")
	}
	binaryPath, err := filepath.Abs(options.BinaryPath)
	if err != nil {
		return PackageResult{}, err
	}
	info, err := os.Lstat(binaryPath)
	if err != nil {
		return PackageResult{}, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxExecutableBytes {
		return PackageResult{}, errors.New("release binary must be a non-empty regular file within the executable size limit")
	}
	outputDir := strings.TrimSpace(options.OutputDir)
	if outputDir == "" {
		outputDir = "."
	}
	outputDir, err = filepath.Abs(outputDir)
	if err != nil {
		return PackageResult{}, err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return PackageResult{}, err
	}
	archiveName := ArchiveName(version, goos, goarch)
	archivePath := filepath.Join(outputDir, archiveName)
	if err := writeArchiveAtomic(archivePath, binaryPath, goos); err != nil {
		return PackageResult{}, err
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return PackageResult{}, err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(hash, archive)
	closeErr := archive.Close()
	if copyErr != nil {
		return PackageResult{}, copyErr
	}
	if closeErr != nil {
		return PackageResult{}, closeErr
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	checksumPath := archivePath + ".sha256"
	line := []byte(digest + "  " + archiveName + "\n")
	if err := writeFileAtomic(checksumPath, line, 0o644); err != nil {
		_ = os.Remove(archivePath)
		return PackageResult{}, err
	}
	return PackageResult{
		Version: version, GOOS: goos, GOARCH: goarch,
		ArchivePath: archivePath, ChecksumPath: checksumPath, SHA256: digest, Size: size,
	}, nil
}

func validReleaseCoordinate(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

func writeArchiveAtomic(destination, binaryPath, goos string) error {
	directory := filepath.Dir(destination)
	temporary, err := os.CreateTemp(directory, ".superscience-package-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if goos == "windows" {
		err = writeZipArchive(temporary, binaryPath)
	} else {
		err = writeTarArchive(temporary, binaryPath)
	}
	if err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return err
	}
	committed = true
	return nil
}

func writeTarArchive(destination io.Writer, binaryPath string) error {
	gzipWriter := gzip.NewWriter(destination)
	gzipWriter.Header.ModTime = time.Unix(0, 0).UTC()
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	info, err := os.Stat(binaryPath)
	if err != nil {
		return err
	}
	header := &tar.Header{
		Name: "superscience", Mode: int64(info.Mode().Perm() | 0o500), Size: info.Size(),
		Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0).UTC(), AccessTime: time.Unix(0, 0).UTC(), ChangeTime: time.Unix(0, 0).UTC(),
	}
	if err := tarWriter.WriteHeader(header); err != nil {
		return err
	}
	if err := copyBinary(tarWriter, binaryPath); err != nil {
		return err
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	return gzipWriter.Close()
}

func writeZipArchive(destination io.Writer, binaryPath string) error {
	zipWriter := zip.NewWriter(destination)
	header := &zip.FileHeader{Name: "superscience.exe", Method: zip.Deflate}
	header.SetMode(0o755)
	header.SetModTime(time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC))
	entry, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}
	if err := copyBinary(entry, binaryPath); err != nil {
		return err
	}
	return zipWriter.Close()
}

func copyBinary(destination io.Writer, binaryPath string) error {
	source, err := os.Open(binaryPath)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(destination, source)
	closeErr := source.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func writeFileAtomic(destination string, payload []byte, mode os.FileMode) error {
	directory := filepath.Dir(destination)
	temporary, err := os.CreateTemp(directory, ".superscience-file-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return fmt.Errorf("publish release file: %w", err)
	}
	committed = true
	return nil
}
