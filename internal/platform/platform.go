// Package platform provides platform-specific service management.
package platform

import (
	"fmt"
	"runtime"
	"strings"

	"nazim/internal/service"
)

func normalizeServiceName(name string) string {
	normalized := name

	normalized = strings.ReplaceAll(normalized, " ", "")
	normalized = strings.ReplaceAll(normalized, "\t", "")
	normalized = strings.ReplaceAll(normalized, "\n", "")
	normalized = strings.ReplaceAll(normalized, "\r", "")

	normalized = strings.ReplaceAll(normalized, "\\", "")
	normalized = strings.ReplaceAll(normalized, "/", "")
	normalized = strings.ReplaceAll(normalized, ":", "")
	normalized = strings.ReplaceAll(normalized, "*", "")
	normalized = strings.ReplaceAll(normalized, "?", "")
	normalized = strings.ReplaceAll(normalized, "\"", "")
	normalized = strings.ReplaceAll(normalized, "<", "")
	normalized = strings.ReplaceAll(normalized, ">", "")
	normalized = strings.ReplaceAll(normalized, "|", "")
	
	return normalized
}

// Manager is an interface for managing services on different platforms.
type Manager interface {
	Install(service *service.Service) error
	Uninstall(name string) error
	Start(name string) error
	Stop(name string) error
	IsInstalled(name string) (bool, error)
}

// NewManager creates an appropriate platform manager for the current OS.
func NewManager() (Manager, error) {
	switch runtime.GOOS {
	case "windows":
		return NewWindowsManager(), nil
	case "linux":
		return NewLinuxManager()
	case "darwin":
		return NewDarwinManager(), nil
	default:
		return nil, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}
