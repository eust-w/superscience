package release

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrConfirmationRequired = errors.New("uninstall requires explicit confirmation")

type UninstallOptions struct {
	Target     string
	DataDir    string
	RemoveData bool
	Confirm    bool
}

type UninstallPlan struct {
	InstallationTarget string `json:"installationTarget"`
	DataDir            string `json:"dataDir,omitempty"`
	RemoveData         bool   `json:"removeData"`
}

type UninstallResult struct {
	UninstallPlan
	RemovedInstallation bool `json:"removedInstallation"`
	RemovedData         bool `json:"removedData"`
}

func PlanUninstall(options UninstallOptions) (UninstallPlan, error) {
	if strings.TrimSpace(options.Target) == "" {
		return UninstallPlan{}, errors.New("uninstall target is required")
	}
	target, err := filepath.Abs(options.Target)
	if err != nil {
		return UninstallPlan{}, err
	}
	target = applicationRoot(target)
	if err := validateRemovalTarget(target); err != nil {
		return UninstallPlan{}, err
	}
	plan := UninstallPlan{InstallationTarget: target, RemoveData: options.RemoveData}
	if strings.TrimSpace(options.DataDir) != "" {
		plan.DataDir, err = filepath.Abs(options.DataDir)
		if err != nil {
			return UninstallPlan{}, err
		}
	}
	if options.RemoveData {
		if plan.DataDir == "" {
			return UninstallPlan{}, errors.New("data directory is required when data removal is requested")
		}
		plan.DataDir, err = canonicalDataRemovalPath(plan.DataDir)
		if err != nil {
			return UninstallPlan{}, err
		}
		if err := validateDataRemoval(plan.DataDir); err != nil {
			return UninstallPlan{}, err
		}
	}
	return plan, nil
}

func Uninstall(options UninstallOptions) (UninstallResult, error) {
	plan, err := PlanUninstall(options)
	if err != nil {
		return UninstallResult{}, err
	}
	if !options.Confirm {
		return UninstallResult{UninstallPlan: plan}, ErrConfirmationRequired
	}
	info, err := os.Lstat(plan.InstallationTarget)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return UninstallResult{}, err
	}
	removedInstallation := false
	if err == nil {
		if info.IsDir() {
			err = os.RemoveAll(plan.InstallationTarget)
		} else {
			err = os.Remove(plan.InstallationTarget)
		}
		if err != nil {
			return UninstallResult{}, err
		}
		removedInstallation = true
	}
	removedData := false
	if plan.RemoveData {
		if _, err := os.Stat(plan.DataDir); err == nil {
			if err := os.RemoveAll(plan.DataDir); err != nil {
				return UninstallResult{}, err
			}
			removedData = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return UninstallResult{}, err
		}
	}
	return UninstallResult{
		UninstallPlan:       plan,
		RemovedInstallation: removedInstallation,
		RemovedData:         removedData,
	}, nil
}

func applicationRoot(executable string) string {
	normalized := filepath.ToSlash(executable)
	marker := ".app/Contents/MacOS/"
	if index := strings.Index(strings.ToLower(normalized), strings.ToLower(marker)); index >= 0 {
		return filepath.FromSlash(normalized[:index+len(".app")])
	}
	return executable
}

func validateRemovalTarget(target string) error {
	if err := validateSafeRemovalPath(target); err != nil {
		return fmt.Errorf("refusing to remove Superscience installation: %w", err)
	}
	info, err := os.Lstat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if !recognizedExecutableName(filepath.Base(target)) && !recognizedApplicationName(filepath.Base(target)) {
				return fmt.Errorf("target is not a Superscience installation: %s", target)
			}
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to remove a symlink as a Superscience installation")
	}
	if info.IsDir() {
		if !recognizedApplicationName(filepath.Base(target)) {
			return fmt.Errorf("target is not a Superscience application: %s", target)
		}
		return nil
	}
	if !info.Mode().IsRegular() || !recognizedExecutableName(filepath.Base(target)) {
		return fmt.Errorf("target is not a Superscience executable: %s", target)
	}
	return nil
}

func validateDataRemoval(dataDir string) error {
	if err := validateSafeRemovalPath(dataDir); err != nil {
		return fmt.Errorf("refusing to remove data directory: %w", err)
	}
	if strings.EqualFold(filepath.Base(dataDir), "superscience") {
		return nil
	}
	marker := filepath.Join(dataDir, "superscience.db")
	if info, err := os.Lstat(marker); err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	return fmt.Errorf("refusing to remove data directory without a Superscience marker: %s", dataDir)
}

func canonicalDataRemovalPath(dataDir string) (string, error) {
	absolute, err := filepath.Abs(dataDir)
	if err != nil {
		return "", err
	}
	for _, candidate := range []string{absolute, filepath.Dir(absolute)} {
		info, statErr := os.Lstat(candidate)
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("refusing to remove data directory through symlink %s", candidate)
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	resolvedParent, parentErr := filepath.EvalSymlinks(filepath.Dir(absolute))
	if parentErr == nil {
		return filepath.Join(resolvedParent, filepath.Base(absolute)), nil
	}
	if !errors.Is(parentErr, os.ErrNotExist) {
		return "", parentErr
	}
	return absolute, nil
}

func validateSafeRemovalPath(target string) error {
	clean := filepath.Clean(target)
	volume := filepath.VolumeName(clean)
	root := string(filepath.Separator)
	if volume != "" {
		root = volume + string(filepath.Separator)
	}
	if clean == "." || clean == root || clean == volume {
		return errors.New("path resolves to a filesystem root")
	}
	if home, err := os.UserHomeDir(); err == nil {
		home, _ = filepath.Abs(home)
		if clean == home {
			return errors.New("path resolves to the user home directory")
		}
	}
	return nil
}

func recognizedExecutableName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return name == "superscience" || name == "superscience.exe"
}

func recognizedApplicationName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "Superscience.app")
}

func executableName(goos string) string {
	if goos == "windows" {
		return "superscience.exe"
	}
	return "superscience"
}
