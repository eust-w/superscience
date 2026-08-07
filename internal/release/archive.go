package release

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func extractExecutable(archive []byte, goos string) ([]byte, os.FileMode, error) {
	if goos == "windows" {
		return extractZipExecutable(archive)
	}
	return extractTarExecutable(archive)
}

func extractTarExecutable(archive []byte) ([]byte, os.FileMode, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, 0, fmt.Errorf("open release archive: %w", err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	var executable []byte
	var mode os.FileMode
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("read release archive: %w", err)
		}
		clean, err := safeArchivePath(header.Name)
		if err != nil {
			return nil, 0, err
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Typeflag != tar.TypeDir {
			return nil, 0, fmt.Errorf("release archive contains unsupported entry %s", clean)
		}
		if clean != "superscience" || header.Typeflag == tar.TypeDir {
			continue
		}
		if executable != nil {
			return nil, 0, errors.New("release archive contains duplicate superscience executables")
		}
		if header.Size < 0 || header.Size > maxExecutableBytes {
			return nil, 0, errors.New("release executable exceeds the size limit")
		}
		executable, err = io.ReadAll(io.LimitReader(reader, maxExecutableBytes+1))
		if err != nil {
			return nil, 0, err
		}
		if int64(len(executable)) > maxExecutableBytes {
			return nil, 0, errors.New("release executable exceeds the size limit")
		}
		mode = os.FileMode(header.Mode).Perm()
	}
	if executable == nil {
		return nil, 0, errors.New("release archive does not contain superscience")
	}
	return executable, mode | 0o500, nil
}

func extractZipExecutable(archive []byte) ([]byte, os.FileMode, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, 0, fmt.Errorf("open release archive: %w", err)
	}
	var executable []byte
	var mode os.FileMode
	for _, file := range reader.File {
		clean, err := safeArchivePath(file.Name)
		if err != nil {
			return nil, 0, err
		}
		if clean != "superscience.exe" || file.FileInfo().IsDir() {
			continue
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return nil, 0, errors.New("release archive contains a symlink executable")
		}
		if !file.Mode().IsRegular() {
			return nil, 0, errors.New("release archive executable is not a regular file")
		}
		if executable != nil {
			return nil, 0, errors.New("release archive contains duplicate superscience executables")
		}
		if file.UncompressedSize64 > maxExecutableBytes {
			return nil, 0, errors.New("release executable exceeds the size limit")
		}
		entry, err := file.Open()
		if err != nil {
			return nil, 0, err
		}
		executable, err = io.ReadAll(io.LimitReader(entry, maxExecutableBytes+1))
		closeErr := entry.Close()
		if err != nil {
			return nil, 0, err
		}
		if closeErr != nil {
			return nil, 0, closeErr
		}
		if int64(len(executable)) > maxExecutableBytes {
			return nil, 0, errors.New("release executable exceeds the size limit")
		}
		mode = file.Mode().Perm()
	}
	if executable == nil {
		return nil, 0, errors.New("release archive does not contain superscience.exe")
	}
	return executable, mode | 0o500, nil
}

func safeArchivePath(name string) (string, error) {
	normalized := strings.ReplaceAll(name, "\\", "/")
	if strings.ContainsRune(name, '\x00') || path.IsAbs(normalized) || strings.HasPrefix(normalized, "/") {
		return "", fmt.Errorf("release archive contains unsafe path %q", name)
	}
	clean := path.Clean(normalized)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, ":") {
		return "", fmt.Errorf("release archive contains unsafe path %q", name)
	}
	return clean, nil
}

func atomicInstall(target string, payload []byte, mode os.FileMode) error {
	absolute, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	directory := filepath.Dir(absolute)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".superscience-upgrade-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	installed := false
	defer func() {
		_ = temporary.Close()
		if !installed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(mode.Perm() | 0o500); err != nil {
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
	if err := os.Rename(temporaryPath, absolute); err != nil {
		return fmt.Errorf("replace Superscience executable: %w", err)
	}
	installed = true
	if directoryHandle, err := os.Open(directory); err == nil {
		_ = directoryHandle.Sync()
		_ = directoryHandle.Close()
	}
	return nil
}
