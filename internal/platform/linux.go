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
	home, err := getHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	userSystemdDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(userSystemdDir, 0755); err != nil {
		return fmt.Errorf("failed to create systemd directory: %w", err)
	}

	normalizedName := normalizeServiceName(svc.Name)
	serviceFile := filepath.Join(userSystemdDir, fmt.Sprintf("nazim-%s.service", normalizedName))

	logDir := filepath.Join(home, ".nazim", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// Build ExecStart with proper escaping to prevent directive injection
	execStartLine, err := escapeSystemdExec(svc.Command, svc.Args)
	if err != nil {
		return fmt.Errorf("failed to escape command: %w", err)
	}

	var content strings.Builder
	content.WriteString("[Unit]\n")
	content.WriteString(fmt.Sprintf("Description=Nazim Service: %s\n", escapeSystemdValue(svc.Name)))
	content.WriteString("After=network.target\n\n")
	content.WriteString("[Service]\n")
	content.WriteString("Type=oneshot\n")
	content.WriteString(execStartLine)
	content.WriteString("\n")
	if svc.WorkDir != "" {
		content.WriteString(fmt.Sprintf("WorkingDirectory=%s\n", escapeSystemdValue(svc.WorkDir)))
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
`, escapeSystemdValue(svc.Name), formatSystemdDuration(interval), formatSystemdDuration(interval))

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
	home, err := getHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	normalizedName := normalizeServiceName(name)
	serviceName := fmt.Sprintf("nazim-%s", normalizedName)
	timerName := fmt.Sprintf("nazim-%s.timer", normalizedName)

	_ = exec.Command("systemctl", "--user", "stop", timerName).Run()
	_ = exec.Command("systemctl", "--user", "disable", timerName).Run()

	_ = exec.Command("systemctl", "--user", "stop", fmt.Sprintf("%s.service", serviceName)).Run()
	_ = exec.Command("systemctl", "--user", "disable", fmt.Sprintf("%s.service", serviceName)).Run()

	userSystemdDir := filepath.Join(home, ".config", "systemd", "user")
	_ = os.Remove(filepath.Join(userSystemdDir, fmt.Sprintf("%s.service", serviceName)))
	_ = os.Remove(filepath.Join(userSystemdDir, timerName))

	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("failed to reload systemd daemon: %w", err)
	}

	return nil
}

// Enable enables a service on Linux (allows it to start automatically).
func (m *LinuxManager) Enable(name string) error {
	normalizedName := normalizeServiceName(name)
	timerName := fmt.Sprintf("nazim-%s.timer", normalizedName)

	cmd := exec.Command("systemctl", "--user", "enable", timerName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}
	return nil
}

// Disable disables a service on Linux (prevents automatic start).
func (m *LinuxManager) Disable(name string) error {
	normalizedName := normalizeServiceName(name)
	timerName := fmt.Sprintf("nazim-%s.timer", normalizedName)

	cmd := exec.Command("systemctl", "--user", "disable", timerName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to disable service: %w", err)
	}
	return nil
}

// Run executes a service immediately on Linux.
func (m *LinuxManager) Run(name string) error {
	normalizedName := normalizeServiceName(name)
	serviceName := fmt.Sprintf("nazim-%s.service", normalizedName)

	cmd := exec.Command("systemctl", "--user", "start", serviceName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run service: %w", err)
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

// GetTaskState returns the state of a systemd timer ("Enabled" or "Disabled").
// Returns error if timer is not found.
func (m *LinuxManager) GetTaskState(name string) (string, error) {
	normalizedName := normalizeServiceName(name)
	timerName := fmt.Sprintf("nazim-%s.timer", normalizedName)

	cmd := exec.Command("systemctl", "--user", "is-enabled", timerName)
	output, err := cmd.CombinedOutput()

	outputStr := strings.TrimSpace(string(output))

	if err != nil {
		// If command failed, timer might not exist or be in a bad state
		if strings.Contains(outputStr, "not found") || strings.Contains(outputStr, "No such file") {
			return "", fmt.Errorf("timer not found")
		}
		// If disabled, is-enabled returns exit code 1 with "disabled" output
		if outputStr == "disabled" {
			return "Disabled", nil
		}
		return "", fmt.Errorf("failed to query timer state: %w", err)
	}

	// is-enabled returns "enabled" if active
	if outputStr == "enabled" || outputStr == "static" {
		return "Enabled", nil
	}

	return "Disabled", nil
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

// escapeSystemdExec escapes a command and arguments for use in systemd ExecStart directives.
// It uses systemd's argument vector format with C-style escaping.
func escapeSystemdExec(command string, args []string) (string, error) {
	// Validate command doesn't contain newlines (directive injection)
	if strings.ContainsAny(command, "\n\r") {
		return "", fmt.Errorf("command cannot contain newlines")
	}

	// Resolve to absolute path if not already
	cmdPath := command
	if !filepath.IsAbs(command) {
		absPath, err := exec.LookPath(command)
		if err != nil {
			// If not in PATH, try making it absolute from current dir
			if absPath, err = filepath.Abs(command); err != nil {
				return "", fmt.Errorf("cannot resolve command path: %w", err)
			}
		}
		cmdPath = absPath
	}

	// Quote the command path
	quotedCmd := quoteSystemdArg(cmdPath)

	// Quote each argument
	var quotedArgs []string
	for _, arg := range args {
		// Validate arg doesn't contain newlines
		if strings.ContainsAny(arg, "\n\r") {
			return "", fmt.Errorf("argument cannot contain newlines: %s", arg)
		}
		quotedArgs = append(quotedArgs, quoteSystemdArg(arg))
	}

	// Build ExecStart line
	parts := append([]string{quotedCmd}, quotedArgs...)
	return "ExecStart=" + strings.Join(parts, " "), nil
}

// quoteSystemdArg quotes a single argument using C-style escaping for systemd.
func quoteSystemdArg(s string) string {
	// C-style escaping for systemd
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")

	// Always quote to prevent shell interpretation
	return "\"" + s + "\""
}

// escapeSystemdValue escapes a value for use in systemd unit files (for Description, WorkingDirectory, etc).
func escapeSystemdValue(s string) string {
	// Remove newlines entirely to prevent directive injection
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}
