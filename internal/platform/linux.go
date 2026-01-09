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
	// Criar diretório de serviços do usuário se não existir
	userSystemdDir := filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user")
	if err := os.MkdirAll(userSystemdDir, 0755); err != nil {
		return fmt.Errorf("failed to create systemd directory: %w", err)
	}

	serviceFile := filepath.Join(userSystemdDir, fmt.Sprintf("nazim-%s.service", svc.Name))

	// Construir conteúdo do arquivo de serviço
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
	content.WriteString("\n")

	// Se for agendado, criar timer
	if svc.GetInterval() > 0 {
		content.WriteString("[Install]\n")
		content.WriteString("WantedBy=default.target\n")

		if err := os.WriteFile(serviceFile, []byte(content.String()), 0644); err != nil {
			return fmt.Errorf("failed to write service file: %w", err)
		}

		// Criar arquivo de timer
		timerFile := filepath.Join(userSystemdDir, fmt.Sprintf("nazim-%s.timer", svc.Name))
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

		// Recarregar e habilitar
		exec.Command("systemctl", "--user", "daemon-reload").Run()
		exec.Command("systemctl", "--user", "enable", fmt.Sprintf("nazim-%s.timer", svc.Name)).Run()
		exec.Command("systemctl", "--user", "start", fmt.Sprintf("nazim-%s.timer", svc.Name)).Run()
	} else if svc.OnStartup && svc.GetInterval() == 0 {
		content.WriteString("[Install]\n")
		content.WriteString("WantedBy=default.target\n")

		if err := os.WriteFile(serviceFile, []byte(content.String()), 0644); err != nil {
			return fmt.Errorf("failed to write service file: %w", err)
		}

		exec.Command("systemctl", "--user", "daemon-reload").Run()
		exec.Command("systemctl", "--user", "enable", fmt.Sprintf("nazim-%s.service", svc.Name)).Run()
	}

	return nil
}

// Uninstall removes a service from Linux.
func (m *LinuxManager) Uninstall(name string) error {
	exec.Command("systemctl", "--user", "stop", fmt.Sprintf("nazim-%s.timer", name)).Run()
	exec.Command("systemctl", "--user", "disable", fmt.Sprintf("nazim-%s.timer", name)).Run()
	exec.Command("systemctl", "--user", "stop", fmt.Sprintf("nazim-%s.service", name)).Run()
	exec.Command("systemctl", "--user", "disable", fmt.Sprintf("nazim-%s.service", name)).Run()

	userSystemdDir := filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user")
	os.Remove(filepath.Join(userSystemdDir, fmt.Sprintf("nazim-%s.service", name)))
	os.Remove(filepath.Join(userSystemdDir, fmt.Sprintf("nazim-%s.timer", name)))
	exec.Command("systemctl", "--user", "daemon-reload").Run()

	return nil
}

// Start starts a service on Linux.
func (m *LinuxManager) Start(name string) error {
	cmd := exec.Command("systemctl", "--user", "start", fmt.Sprintf("nazim-%s.service", name))
	return cmd.Run()
}

// Stop stops a service on Linux.
func (m *LinuxManager) Stop(name string) error {
	cmd := exec.Command("systemctl", "--user", "stop", fmt.Sprintf("nazim-%s.service", name))
	return cmd.Run()
}

// IsInstalled checks if a service is installed.
func (m *LinuxManager) IsInstalled(name string) (bool, error) {
	cmd := exec.Command("systemctl", "--user", "is-enabled", fmt.Sprintf("nazim-%s.service", name))
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
