// Package platform provides platform-specific service management.
package platform

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/calilkhalil/nazim/internal/service"
)

// getHomeDir safely retrieves the user's home directory with validation.
// It ensures the directory is non-empty and exists, falling back to current directory if needed.
func getHomeDir() (string, error) {
	// Try os.UserHomeDir() first (most reliable)
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		// Validate it's an absolute path
		if len(home) > 0 && (home[0] == '/' || (len(home) > 1 && home[1] == ':')) {
			return home, nil
		}
	}

	// Try HOME environment variable as fallback (Unix)
	if runtime.GOOS != "windows" {
		if home := os.Getenv("HOME"); home != "" && (home[0] == '/') {
			return home, nil
		}
	}

	// Try USERPROFILE on Windows
	if runtime.GOOS == "windows" {
		if home := os.Getenv("USERPROFILE"); home != "" && len(home) > 1 && home[1] == ':' {
			return home, nil
		}
	}

	// Last resort: current directory
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("unable to determine home directory or current directory")
	}
	return cwd, nil
}

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
	Enable(name string) error
	Disable(name string) error
	Run(name string) error
	IsInstalled(name string) (bool, error)
	GetTaskState(name string) (string, error) // Returns "Enabled", "Disabled", or error if not found
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
