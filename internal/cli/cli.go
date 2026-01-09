// Package cli handles command-line interface operations.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"nazim/internal/config"
	"nazim/internal/platform"
	"nazim/internal/service"
)

// CLI handles command-line operations.
type CLI struct {
	cfg *config.Config
}

// New creates a new CLI instance.
func New(cfg *config.Config) *CLI {
	return &CLI{cfg: cfg}
}

// Flags holds command-line flags for add command.
type Flags struct {
	Name      string
	Command   string
	Args      string
	WorkDir   string
	OnStartup bool
	Interval  string
}

// Add adds a new service.
func (c *CLI) Add(ctx context.Context, flags *Flags, verbose bool) error {
	// Check if command is "write" or "edit" - open interactive editor
	command := flags.Command
	if command == "write" || command == "edit" {
		scriptPath, err := c.createScriptInteractive(flags.Name, verbose)
		if err != nil {
			return fmt.Errorf("failed to create script: %w", err)
		}
		command = scriptPath
	}

	// Parse interval
	var intervalDuration time.Duration
	if flags.Interval != "" {
		var err error
		intervalDuration, err = parseDuration(flags.Interval)
		if err != nil {
			return fmt.Errorf("invalid interval format: %w", err)
		}
	}

	// Parse args
	var args []string
	if flags.Args != "" {
		args = strings.Fields(flags.Args)
	}

	// Create service
	svc := &service.Service{
		Name:      flags.Name,
		Command:   command,
		Args:      args,
		WorkDir:   flags.WorkDir,
		OnStartup: flags.OnStartup,
		Interval:  service.Duration{Duration: intervalDuration},
		Enabled:   true,
		Platform:  runtime.GOOS,
	}

	// Validate
	if err := svc.Validate(); err != nil {
		return err
	}

	// Add to config
	if err := c.cfg.AddService(svc); err != nil {
		return fmt.Errorf("failed to add service: %w", err)
	}

	if verbose {
		fmt.Printf("Service '%s' added to configuration.\n", flags.Name)
	}

	// Install on system
	platformMgr, err := platform.NewManager()
	if err != nil {
		// Remove from config if install fails
		_ = c.cfg.RemoveService(flags.Name)
		return fmt.Errorf("failed to create platform manager: %w", err)
	}

	if err := platformMgr.Install(svc); err != nil {
		// Remove from config if install fails
		_ = c.cfg.RemoveService(flags.Name)
		return fmt.Errorf("failed to install service: %w", err)
	}

	fmt.Printf("Service '%s' installed successfully!\n", flags.Name)
	return nil
}

// List lists all services.
func (c *CLI) List(ctx context.Context, verbose bool) error {
	services := c.cfg.ListServices()
	if len(services) == 0 {
		fmt.Println("No services found.")
		return nil
	}

	platformMgr, err := platform.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create platform manager: %w", err)
	}

	// Simple table output
	fmt.Println("NAME\tCOMMAND\tTYPE\tSTATUS")
	fmt.Println("----\t-------\t----\t------")

	for _, svc := range services {
		installed, _ := platformMgr.IsInstalled(svc.Name)
		status := "Not Installed"
		if installed {
			status = "Installed"
		}

		svcType := ""
		if svc.OnStartup {
			svcType = "Startup"
		}
		if svc.GetInterval() > 0 {
			if svcType != "" {
				svcType += " + "
			}
			svcType += fmt.Sprintf("Every %s", formatDuration(svc.GetInterval()))
		}

		cmdStr := svc.Command
		if len(svc.Args) > 0 {
			cmdStr += " " + strings.Join(svc.Args, " ")
		}

		fmt.Printf("%s\t%s\t%s\t%s\n", svc.Name, cmdStr, svcType, status)
	}

	return nil
}

// Remove removes a service.
func (c *CLI) Remove(ctx context.Context, name string, verbose bool) error {
	// Check if exists
	if _, err := c.cfg.GetService(name); err != nil {
		return fmt.Errorf("service '%s' does not exist", name)
	}

	// Uninstall from system
	platformMgr, err := platform.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create platform manager: %w", err)
	}

	if err := platformMgr.Uninstall(name); err != nil {
		if verbose {
			fmt.Printf("Warning: failed to uninstall service from system: %v\n", err)
		}
	}

	// Remove from config
	if err := c.cfg.RemoveService(name); err != nil {
		return fmt.Errorf("failed to remove service: %w", err)
	}

	fmt.Printf("Service '%s' removed successfully!\n", name)
	return nil
}

// Start starts a service.
func (c *CLI) Start(ctx context.Context, name string, verbose bool) error {
	if _, err := c.cfg.GetService(name); err != nil {
		return fmt.Errorf("service '%s' does not exist", name)
	}

	platformMgr, err := platform.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create platform manager: %w", err)
	}

	if err := platformMgr.Start(name); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	fmt.Printf("Service '%s' started successfully!\n", name)
	return nil
}

// Stop stops a service.
func (c *CLI) Stop(ctx context.Context, name string, verbose bool) error {
	if _, err := c.cfg.GetService(name); err != nil {
		return fmt.Errorf("service '%s' does not exist", name)
	}

	platformMgr, err := platform.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create platform manager: %w", err)
	}

	if err := platformMgr.Stop(name); err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}

	fmt.Printf("Service '%s' stopped successfully!\n", name)
	return nil
}

// parseDuration parses a duration string (e.g., "5m", "1h", "30s").
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)

	var multiplier time.Duration
	switch {
	case strings.HasSuffix(s, "s"):
		multiplier = time.Second
		s = strings.TrimSuffix(s, "s")
	case strings.HasSuffix(s, "m"):
		multiplier = time.Minute
		s = strings.TrimSuffix(s, "m")
	case strings.HasSuffix(s, "h"):
		multiplier = time.Hour
		s = strings.TrimSuffix(s, "h")
	case strings.HasSuffix(s, "d"):
		multiplier = 24 * time.Hour
		s = strings.TrimSuffix(s, "d")
	default:
		return 0, fmt.Errorf("invalid duration suffix, use s, m, h, or d")
	}

	var value int
	if _, err := fmt.Sscanf(s, "%d", &value); err != nil {
		return 0, fmt.Errorf("invalid duration value: %w", err)
	}

	return time.Duration(value) * multiplier, nil
}

// formatDuration formats a duration for display.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// createScriptInteractive opens the default editor to create a script.
func (c *CLI) createScriptInteractive(serviceName string, verbose bool) (string, error) {
	scriptsDir := c.cfg.GetScriptsDir()
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		return "", fmt.Errorf("creating scripts directory: %w", err)
	}

	// Determine script extension based on OS
	var ext string
	switch runtime.GOOS {
	case "windows":
		ext = ".bat"
	case "darwin", "linux":
		ext = ".sh"
	default:
		ext = ".sh"
	}

	scriptPath := filepath.Join(scriptsDir, fmt.Sprintf("%s%s", serviceName, ext))

	// Get editor from environment or use defaults
	editor := getEditor()

	if verbose {
		fmt.Printf("Opening editor: %s\n", editor)
		fmt.Printf("Script will be saved to: %s\n", scriptPath)
	}

	// Create initial script content with template
	var initialContent string
	switch runtime.GOOS {
	case "windows":
		initialContent = createWindowsTemplate(serviceName)
	case "darwin", "linux":
		initialContent = createUnixTemplate(serviceName)
	default:
		initialContent = createUnixTemplate(serviceName)
	}

	// Write initial content
	if err := os.WriteFile(scriptPath, []byte(initialContent), 0644); err != nil {
		return "", fmt.Errorf("creating script file: %w", err)
	}

	// Make executable on Unix-like systems
	if runtime.GOOS != "windows" {
		if err := os.Chmod(scriptPath, 0755); err != nil {
			return "", fmt.Errorf("making script executable: %w", err)
		}
	}

	// Open editor
	var cmd *exec.Cmd
	if editor == "open" && runtime.GOOS == "darwin" {
		// macOS: use open -e for TextEdit, or open -a for specific app
		// Try to use the default editor via open -t (text editor)
		cmd = exec.Command("open", "-t", scriptPath)
	} else {
		cmd = exec.Command(editor, scriptPath)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// If editor fails, remove the file
		os.Remove(scriptPath)
		return "", fmt.Errorf("editor failed: %w", err)
	}

	// Verify file was created and has content
	info, err := os.Stat(scriptPath)
	if err != nil {
		return "", fmt.Errorf("script file not found: %w", err)
	}

	if info.Size() == 0 {
		return "", fmt.Errorf("script file is empty")
	}

	if verbose {
		fmt.Printf("Script created successfully: %s\n", scriptPath)
	}

	return scriptPath, nil
}

// getEditor returns the editor command to use.
func getEditor() string {
	// Try EDITOR environment variable first
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}

	// Try VISUAL environment variable
	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor
	}

	// Platform-specific defaults
	switch runtime.GOOS {
	case "windows":
		// Try common Windows editors
		if _, err := exec.LookPath("code"); err == nil {
			return "code"
		}
		if _, err := exec.LookPath("notepad"); err == nil {
			return "notepad"
		}
		return "notepad" // Fallback
	case "darwin":
		// macOS
		if _, err := exec.LookPath("code"); err == nil {
			return "code"
		}
		if _, err := exec.LookPath("vim"); err == nil {
			return "vim"
		}
		if _, err := exec.LookPath("nano"); err == nil {
			return "nano"
		}
		// Use open -e for TextEdit, or fallback to vim
		if _, err := exec.LookPath("open"); err == nil {
			// We'll handle "open" specially in createScriptInteractive
			return "open"
		}
		return "vim" // Fallback
	case "linux":
		// Linux
		if _, err := exec.LookPath("code"); err == nil {
			return "code"
		}
		if _, err := exec.LookPath("vim"); err == nil {
			return "vim"
		}
		if _, err := exec.LookPath("nano"); err == nil {
			return "nano"
		}
		if _, err := exec.LookPath("vi"); err == nil {
			return "vi"
		}
		return "nano" // Fallback
	default:
		return "vi" // Ultimate fallback
	}
}

// createUnixTemplate creates a template for Unix-like systems (Linux/macOS).
func createUnixTemplate(serviceName string) string {
	return fmt.Sprintf(`#!/bin/sh
# ============================================================================
# nazim Service Script
# ============================================================================
# Service Name: %s
# Created: %s
# Location: ~/.config/nazim/scripts/%s.sh
# 
# This script is managed by nazim. Write your service code in the section
# marked "YOUR CODE HERE" below.
# ============================================================================

# ============================================================================
# YOUR CODE HERE
# ============================================================================
# Write your service logic below this line:

echo "Service %s is running..."

# Example commands you can use:
# - Run a backup: tar -czf backup.tar.gz /path/to/data
# - Process files: find /path -name "*.log" -exec process.sh {} \;
# - Send notifications: curl -X POST https://api.example.com/notify
# - Clean up: rm -rf /tmp/old_files
# - Run Python script: python3 /path/to/script.py
# - Execute other scripts: /path/to/other-script.sh

# ============================================================================
# END OF YOUR CODE
# ============================================================================

# Exit with status code (0 = success, non-zero = failure)
exit 0
`, serviceName, time.Now().Format("2006-01-02 15:04:05"), serviceName, serviceName)
}

// createWindowsTemplate creates a template for Windows systems.
func createWindowsTemplate(serviceName string) string {
	return fmt.Sprintf(`@echo off
REM ============================================================================
REM nazim Service Script
REM ============================================================================
REM Service Name: %s
REM Created: %s
REM Location: %%APPDATA%%\nazim\scripts\%s.bat
REM 
REM This script is managed by nazim. Write your service code in the section
REM marked "YOUR CODE HERE" below.
REM ============================================================================

REM ============================================================================
REM YOUR CODE HERE
REM ============================================================================
REM Write your service logic below this line:

echo Service %s is running...

REM Example commands you can use:
REM - Run a backup: xcopy "C:\data" "D:\backup" /E /I /Y
REM - Process files: for %%f in (C:\logs\*.log) do process.bat "%%f"
REM - Send notifications: powershell -Command "Invoke-WebRequest -Uri https://api.example.com/notify"
REM - Clean up: del /Q "C:\temp\*.tmp"
REM - Run Python script: python C:\path\to\script.py
REM - Execute other scripts: call C:\path\to\other-script.bat

REM ============================================================================
REM END OF YOUR CODE
REM ============================================================================

REM Exit with status code (0 = success, non-zero = failure)
exit /b 0
`, serviceName, time.Now().Format("2006-01-02 15:04:05"), serviceName, serviceName)
}
