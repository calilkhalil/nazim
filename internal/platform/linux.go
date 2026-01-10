// Package platform provides Linux-specific service management.
package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"nazim/internal/service"
)

// LinuxManager manages services on Linux using systemd.
type LinuxManager struct{}

// NewLinuxManager creates a new manager for Linux.
func NewLinuxManager() (*LinuxManager, error) {
	// Verify systemd is available
	if err := exec.Command("systemctl", "--version").Run(); err != nil {
		return nil, fmt.Errorf("systemd is not available: %w", err)
	}
	return &LinuxManager{}, nil
}

// Install installs a service on Linux using systemd.
func (m *LinuxManager) Install(svc *service.Service) error {
	return m.installSystemd(svc)
}

func (m *LinuxManager) installSystemd(svc *service.Service) error {
	userSystemdDir := filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user")
	if err := os.MkdirAll(userSystemdDir, 0755); err != nil {
		return fmt.Errorf("failed to create systemd directory: %w", err)
	}

	normalizedName := normalizeServiceName(svc.Name)
	serviceFile := filepath.Join(userSystemdDir, fmt.Sprintf("nazim-%s.service", normalizedName))

	logDir := filepath.Join(os.Getenv("HOME"), ".nazim", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	var content strings.Builder
	content.WriteString("[Unit]\n")
	content.WriteString(fmt.Sprintf("Description=Nazim Service: %s\n", svc.Name))
	content.WriteString("After=network.target\n\n")
	content.WriteString("[Service]\n")
	content.WriteString("Type=oneshot\n")
	content.WriteString(fmt.Sprintf("ExecStart=%s", svc.Command))
	if len(svc.Args) > 0 {
		content.WriteString(" " + strings.Join(svc.Args, " "))
	}
	content.WriteString("\n")
	if svc.WorkDir != "" {
		content.WriteString(fmt.Sprintf("WorkingDirectory=%s\n", svc.WorkDir))
	}
	normalizedNameForLog := normalizeServiceName(svc.Name)
	logPath := filepath.Join(logDir, fmt.Sprintf("%s.log", normalizedNameForLog))
	content.WriteString(fmt.Sprintf("StandardOutput=append:%s\n", logPath))
	content.WriteString(fmt.Sprintf("StandardError=append:%s\n", logPath))
	content.WriteString("\n")

	if svc.GetInterval() > 0 {
		content.WriteString("[Install]\n")
		content.WriteString("WantedBy=default.target\n")

		if err := os.WriteFile(serviceFile, []byte(content.String()), 0644); err != nil {
			return fmt.Errorf("failed to write service file: %w", err)
		}

		timerFile := filepath.Join(userSystemdDir, fmt.Sprintf("nazim-%s.timer", normalizedName))
		interval := svc.GetInterval()
		timerContent := fmt.Sprintf(`[Unit]
Description=Timer for Nazim Service: %s

[Timer]
OnBootSec=%s
OnUnitActiveSec=%s

[Install]
WantedBy=timers.target
`, svc.Name, formatSystemdDuration(interval), formatSystemdDuration(interval))

		if err := os.WriteFile(timerFile, []byte(timerContent), 0644); err != nil {
			return fmt.Errorf("failed to write timer file: %w", err)
		}

		if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
			return fmt.Errorf("failed to reload systemd daemon: %w", err)
		}
		if err := exec.Command("systemctl", "--user", "enable", fmt.Sprintf("nazim-%s.timer", normalizedName)).Run(); err != nil {
			return fmt.Errorf("failed to enable timer: %w", err)
		}
		if err := exec.Command("systemctl", "--user", "start", fmt.Sprintf("nazim-%s.timer", normalizedName)).Run(); err != nil {
			return fmt.Errorf("failed to start timer: %w", err)
		}
	} else if svc.OnStartup && svc.GetInterval() == 0 {
		content.WriteString("[Install]\n")
		content.WriteString("WantedBy=default.target\n")

		if err := os.WriteFile(serviceFile, []byte(content.String()), 0644); err != nil {
			return fmt.Errorf("failed to write service file: %w", err)
		}

		if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
			return fmt.Errorf("failed to reload systemd daemon: %w", err)
		}
		if err := exec.Command("systemctl", "--user", "enable", fmt.Sprintf("nazim-%s.service", normalizedName)).Run(); err != nil {
			return fmt.Errorf("failed to enable service: %w", err)
		}
	}

	return nil
}

// Uninstall removes a service from Linux.
func (m *LinuxManager) Uninstall(name string) error {
	normalizedName := normalizeServiceName(name)
	serviceName := fmt.Sprintf("nazim-%s", normalizedName)
	timerName := fmt.Sprintf("nazim-%s.timer", normalizedName)

	_ = exec.Command("systemctl", "--user", "stop", timerName).Run()
	_ = exec.Command("systemctl", "--user", "disable", timerName).Run()

	_ = exec.Command("systemctl", "--user", "stop", fmt.Sprintf("%s.service", serviceName)).Run()
	_ = exec.Command("systemctl", "--user", "disable", fmt.Sprintf("%s.service", serviceName)).Run()

	userSystemdDir := filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user")
	_ = os.Remove(filepath.Join(userSystemdDir, fmt.Sprintf("%s.service", serviceName)))
	_ = os.Remove(filepath.Join(userSystemdDir, timerName))

	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("failed to reload systemd daemon: %w", err)
	}

	return nil
}

// Start starts a service on Linux.
func (m *LinuxManager) Start(name string) error {
	normalizedName := normalizeServiceName(name)
	serviceName := fmt.Sprintf("nazim-%s.service", normalizedName)

	cmd := exec.Command("systemctl", "--user", "start", serviceName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}
	return nil
}

// Stop stops a service on Linux.
func (m *LinuxManager) Stop(name string) error {
	normalizedName := normalizeServiceName(name)
	serviceName := fmt.Sprintf("nazim-%s.service", normalizedName)

	cmd := exec.Command("systemctl", "--user", "stop", serviceName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}
	return nil
}

// IsInstalled checks if a service is installed.
func (m *LinuxManager) IsInstalled(name string) (bool, error) {
	normalizedName := normalizeServiceName(name)
	serviceName := fmt.Sprintf("nazim-%s.service", normalizedName)

	cmd := exec.Command("systemctl", "--user", "is-enabled", serviceName)
	err := cmd.Run()
	return err == nil, nil
}

func formatSystemdDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}
