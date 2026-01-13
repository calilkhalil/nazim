// Package platform provides macOS-specific service management.
package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/calilkhalil/nazim/internal/service"
)

// DarwinManager manages services on macOS using launchd.
type DarwinManager struct{}

// NewDarwinManager creates a new manager for macOS.
func NewDarwinManager() *DarwinManager {
	return &DarwinManager{}
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// Install installs a service on macOS using launchd.
func (m *DarwinManager) Install(svc *service.Service) error {
	home, err := getHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	launchAgentsDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(launchAgentsDir, 0755); err != nil {
		return fmt.Errorf("failed to create LaunchAgents directory: %w", err)
	}

	normalizedName := normalizeServiceName(svc.Name)
	plistFile := filepath.Join(launchAgentsDir, fmt.Sprintf("com.nazim.%s.plist", normalizedName))

	var content strings.Builder
	content.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	content.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	content.WriteString("<plist version=\"1.0\">\n")
	content.WriteString("<dict>\n")
	content.WriteString(fmt.Sprintf("  <key>Label</key>\n  <string>com.nazim.%s</string>\n", normalizedName))
	content.WriteString("  <key>ProgramArguments</key>\n")
	content.WriteString("  <array>\n")

	escapedCmd := escapeXML(svc.Command)
	content.WriteString(fmt.Sprintf("    <string>%s</string>\n", escapedCmd))

	for _, arg := range svc.Args {
		escapedArg := escapeXML(arg)
		content.WriteString(fmt.Sprintf("    <string>%s</string>\n", escapedArg))
	}
	content.WriteString("  </array>\n")

	if svc.WorkDir != "" {
		escapedWorkDir := escapeXML(svc.WorkDir)
		content.WriteString(fmt.Sprintf("  <key>WorkingDirectory</key>\n  <string>%s</string>\n", escapedWorkDir))
	}

	// Support both OnStartup and OnLogon (macOS LaunchAgents run at user login)
	if svc.OnStartup || svc.OnLogon {
		content.WriteString("  <key>RunAtLoad</key>\n  <true/>\n")
	}

	if svc.GetInterval() > 0 {
		content.WriteString("  <key>StartInterval</key>\n")
		content.WriteString(fmt.Sprintf("  <integer>%d</integer>\n", int(svc.GetInterval().Seconds())))
	}

	normalizedNameForLog := normalizeServiceName(svc.Name)
	logDir := filepath.Join(home, ".nazim", "logs")
	content.WriteString("  <key>StandardOutPath</key>\n")
	content.WriteString(fmt.Sprintf("  <string>%s</string>\n", filepath.Join(logDir, fmt.Sprintf("%s.out", normalizedNameForLog))))
	content.WriteString("  <key>StandardErrorPath</key>\n")
	content.WriteString(fmt.Sprintf("  <string>%s</string>\n", filepath.Join(logDir, fmt.Sprintf("%s.err", normalizedNameForLog))))

	content.WriteString("</dict>\n")
	content.WriteString("</plist>\n")

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	if err := os.WriteFile(plistFile, []byte(content.String()), 0644); err != nil {
		return fmt.Errorf("failed to write plist file: %w", err)
	}

	cmd := exec.Command("launchctl", "load", plistFile)
	if err := cmd.Run(); err != nil {
		_ = exec.Command("launchctl", "unload", plistFile).Run()
		cmd = exec.Command("launchctl", "load", plistFile)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to load service: %w", err)
		}
	}

	return nil
}

// Uninstall removes a service from macOS.
func (m *DarwinManager) Uninstall(name string) error {
	home, err := getHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	normalizedName := normalizeServiceName(name)
	plistFile := filepath.Join(home, "Library", "LaunchAgents", fmt.Sprintf("com.nazim.%s.plist", normalizedName))

	_ = exec.Command("launchctl", "unload", plistFile).Run()

	if err := os.Remove(plistFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove plist file: %w", err)
	}

	return nil
}

// Enable enables a service on macOS (loads the launchd agent).
func (m *DarwinManager) Enable(name string) error {
	home, err := getHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	normalizedName := normalizeServiceName(name)
	plistPath := filepath.Join(home, "Library", "LaunchAgents", fmt.Sprintf("com.nazim.%s.plist", normalizedName))

	cmd := exec.Command("launchctl", "load", plistPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to enable service: %s: %w", string(output), err)
	}
	return nil
}

// Disable disables a service on macOS (unloads the launchd agent).
func (m *DarwinManager) Disable(name string) error {
	home, err := getHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	normalizedName := normalizeServiceName(name)
	plistPath := filepath.Join(home, "Library", "LaunchAgents", fmt.Sprintf("com.nazim.%s.plist", normalizedName))

	cmd := exec.Command("launchctl", "unload", plistPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := strings.ToLower(string(output))
		if strings.Contains(outputStr, "could not find service") {
			return nil
		}
		return fmt.Errorf("failed to disable service: %s: %w", string(output), err)
	}
	return nil
}

// Run executes a service immediately on macOS.
func (m *DarwinManager) Run(name string) error {
	normalizedName := normalizeServiceName(name)
	label := fmt.Sprintf("com.nazim.%s", normalizedName)

	cmd := exec.Command("launchctl", "start", label)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run service: %s: %w", string(output), err)
	}
	return nil
}

// IsInstalled checks if a service is installed.
func (m *DarwinManager) IsInstalled(name string) (bool, error) {
	home, err := getHomeDir()
	if err != nil {
		return false, fmt.Errorf("failed to get home directory: %w", err)
	}

	normalizedName := normalizeServiceName(name)
	plistFile := filepath.Join(home, "Library", "LaunchAgents", fmt.Sprintf("com.nazim.%s.plist", normalizedName))

	_, err = os.Stat(plistFile)
	return err == nil, nil
}

// GetTaskState returns the state of a launchd agent ("Enabled" or "Disabled").
// Returns error if agent is not found.
func (m *DarwinManager) GetTaskState(name string) (string, error) {
	home, err := getHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	normalizedName := normalizeServiceName(name)
	label := fmt.Sprintf("com.nazim.%s", normalizedName)

	// Check if agent is loaded (enabled)
	cmd := exec.Command("launchctl", "list")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to query launchd: %w", err)
	}

	// If the label appears in the list, it's loaded (enabled)
	if strings.Contains(string(output), label) {
		return "Enabled", nil
	}

	// Check if plist file exists (installed but not loaded = disabled)
	plistFile := filepath.Join(home, "Library", "LaunchAgents", fmt.Sprintf("com.nazim.%s.plist", normalizedName))
	if _, err := os.Stat(plistFile); err == nil {
		return "Disabled", nil
	}

	return "", fmt.Errorf("agent not found")
}
