package secrets

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

const service = "superscience"

type Backend interface {
	Store(account, secret string) error
	Delete(account string) error
	Load(account string) (string, error)
}

type OSBackend struct{}

func (OSBackend) Store(account, secret string) error {
	if secret == "" {
		return nil
	}
	if runtime.GOOS == "darwin" {
		return exec.Command("security", "add-generic-password", "-U", "-s", service, "-a", account, "-w", secret).Run()
	}
	return fmt.Errorf("system credential storage is unavailable on %s", runtime.GOOS)
}

func (OSBackend) Delete(account string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("security", "delete-generic-password", "-s", service, "-a", account).Run()
	}
	return fmt.Errorf("system credential storage is unavailable on %s", runtime.GOOS)
}

func (OSBackend) Load(account string) (string, error) {
	if account == "" {
		return "", errors.New("credential account is required")
	}
	if runtime.GOOS == "darwin" {
		output, err := exec.Command("security", "find-generic-password", "-w", "-s", service, "-a", account).Output()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(output)), nil
	}
	return "", fmt.Errorf("system credential storage is unavailable on %s", runtime.GOOS)
}

var defaultBackend Backend = OSBackend{}

func Store(account, secret string) error  { return defaultBackend.Store(account, secret) }
func Delete(account string) error         { return defaultBackend.Delete(account) }
func Load(account string) (string, error) { return defaultBackend.Load(account) }
