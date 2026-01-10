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
	"syscall"
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
	// Validate required fields early
	if flags.Name == "" {
		return fmt.Errorf("service name is required (use --name or -n)")
	}
	if flags.Command == "" {
		return fmt.Errorf("service command is required (use --command or -c, or 'write'/'edit' for interactive mode)")
	}

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

	// Prepare data for table
	type rowData struct {
		name    string
		command string
		svcType string
		status  string
	}

	rows := make([]rowData, 0, len(services))
	for _, svc := range services {
		installed, err := platformMgr.IsInstalled(svc.Name)
		status := "Not Installed"
		if installed {
			status = "Installed"
		} else if err != nil {
			// If there's an error checking, show it (might be permission issue)
			status = "Unknown"
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
		if svcType == "" {
			svcType = "-"
		}

		cmdStr := svc.Command
		if len(svc.Args) > 0 {
			cmdStr += " " + strings.Join(svc.Args, " ")
		}

		rows = append(rows, rowData{
			name:    svc.Name,
			command: cmdStr,
			svcType: svcType,
			status:  status,
		})
	}

	// Truncate long commands for display (keep first 50 chars)
	const maxCmdDisplay = 50
	for i := range rows {
		if len(rows[i].command) > maxCmdDisplay {
			rows[i].command = rows[i].command[:maxCmdDisplay] + "..."
		}
	}

	// Calculate column widths
	maxNameLen := 4 // "NAME"
	maxCmdLen := 7  // "COMMAND"
	maxTypeLen := 4 // "TYPE"
	maxStatusLen := 6 // "STATUS"

	for _, row := range rows {
		if len(row.name) > maxNameLen {
			maxNameLen = len(row.name)
		}
		if len(row.command) > maxCmdLen {
			maxCmdLen = len(row.command)
		}
		if len(row.svcType) > maxTypeLen {
			maxTypeLen = len(row.svcType)
		}
		if len(row.status) > maxStatusLen {
			maxStatusLen = len(row.status)
		}
	}

	// Print table header
	headerSeparator := strings.Repeat("-", maxNameLen+maxCmdLen+maxTypeLen+maxStatusLen+13)
	fmt.Println(headerSeparator)
	fmt.Printf("| %-*s | %-*s | %-*s | %-*s |\n",
		maxNameLen, "NAME",
		maxCmdLen, "COMMAND",
		maxTypeLen, "TYPE",
		maxStatusLen, "STATUS")
	fmt.Println(headerSeparator)

	// Print table rows
	for _, row := range rows {
		fmt.Printf("| %-*s | %-*s | %-*s | %-*s |\n",
			maxNameLen, row.name,
			maxCmdLen, row.command,
			maxTypeLen, row.svcType,
			maxStatusLen, row.status)
	}

	fmt.Println(headerSeparator)
	fmt.Printf("\nTotal: %d service(s)\n", len(services))

	return nil
}

// Remove removes a service completely (from system, config, and scripts).
func (c *CLI) Remove(ctx context.Context, name string, verbose bool) error {
	// Check if exists and get service info
	svc, err := c.cfg.GetService(name)
	if err != nil {
		return fmt.Errorf("service '%s' does not exist", name)
	}

	// Uninstall from system
	platformMgr, err := platform.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create platform manager: %w", err)
	}

	if err := platformMgr.Uninstall(name); err != nil {
		// Always show error, not just in verbose mode
		fmt.Fprintf(os.Stderr, "Warning: failed to uninstall service from system: %v\n", err)
		// Continue to remove from config anyway
	}

	// Remove script file if it exists (created by write/edit command)
	scriptsDir := c.cfg.GetScriptsDir()
	var ext string
	switch runtime.GOOS {
	case "windows":
		ext = ".bat"
	case "darwin", "linux":
		ext = ".sh"
	default:
		ext = ".sh"
	}
	scriptPath := filepath.Join(scriptsDir, fmt.Sprintf("%s%s", name, ext))
	
	// Check if the service command points to this script (created by write/edit)
	if svc.Command == scriptPath || strings.HasSuffix(svc.Command, scriptPath) {
		if err := os.Remove(scriptPath); err != nil && !os.IsNotExist(err) {
			if verbose {
				fmt.Fprintf(os.Stderr, "Warning: failed to remove script file: %v\n", err)
			}
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
	
	if s == "" {
		return 0, fmt.Errorf("duration cannot be empty")
	}

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
	
	// Validate value is positive
	if value <= 0 {
		return 0, fmt.Errorf("duration value must be positive, got %d", value)
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

	// Open editor with file monitoring for Windows Notepad
	if runtime.GOOS == "windows" && editor == "notepad" {
		return c.openNotepadWithMonitoring(scriptPath, verbose)
	}

	// Open editor normally for other editors
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
# Service: %s
# Created: %s

# Your code here:

exit 0
`, serviceName, time.Now().Format("2006-01-02 15:04:05"))
}

// createWindowsTemplate creates a template for Windows systems.
func createWindowsTemplate(serviceName string) string {
	// Get current user
	username := os.Getenv("USERNAME")
	if username == "" {
		username = os.Getenv("USER")
	}
	if username == "" {
		username = "unknown"
	}

	// Get local and UTC time
	now := time.Now()
	localTime := now.Format("2006-01-02 15:04:05 MST")
	utcTime := now.UTC().Format("2006-01-02 15:04:05 UTC")

	return fmt.Sprintf(`@echo off
REM Service: %s
REM Created: %s (Local) / %s (UTC)
REM Created by: %s

REM Your code here:

exit /b 0
`, serviceName, localTime, utcTime, username)
}

// openNotepadWithMonitoring opens Notepad and monitors the file, closing Notepad when saved.
func (c *CLI) openNotepadWithMonitoring(scriptPath string, verbose bool) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Get initial file modification time
	initialInfo, err := os.Stat(scriptPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}
	initialModTime := initialInfo.ModTime()

	// Start Notepad
	cmd := exec.CommandContext(ctx, "notepad", scriptPath)
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start notepad: %w", err)
	}

	notepadPID := cmd.Process.Pid

	// Monitor file for changes in a goroutine with context
	fileSaved := make(chan bool, 1)
	go func() {
		defer close(fileSaved)
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				fileSaved <- false
				return
			case <-ticker.C:
				// Check if Notepad process is still running (Windows-specific)
				if runtime.GOOS == "windows" {
					checkCmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", notepadPID), "/NH")
					output, _ := checkCmd.Output()
					if !strings.Contains(strings.ToLower(string(output)), fmt.Sprintf("%d", notepadPID)) {
						// Process has exited
						fileSaved <- false
						return
					}
				} else {
					// Unix-like: try to signal the process
					if err := cmd.Process.Signal(os.Signal(syscall.Signal(0))); err != nil {
						// Process has exited
						fileSaved <- false
						return
					}
				}

				// Check if file was modified
				info, err := os.Stat(scriptPath)
				if err == nil && !info.ModTime().Equal(initialModTime) {
					// File was saved, wait a bit more to ensure it's fully saved
					time.Sleep(500 * time.Millisecond)
					fileSaved <- true
					return
				}
			}
		}
	}()

	// Wait for file to be saved or Notepad to close
	saved := false
	select {
	case result := <-fileSaved:
		saved = result
	case <-ctx.Done():
		return "", fmt.Errorf("timeout waiting for file save")
	}

	if saved {
		// Close Notepad after a short delay
		time.Sleep(300 * time.Millisecond)
		closeNotepad(notepadPID)
	}

	// Wait for Notepad to close
	_, _ = cmd.Process.Wait() // Ignore error and state, process may have already exited

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

// closeNotepad closes Notepad by PID on Windows.
func closeNotepad(pid int) {
	if runtime.GOOS != "windows" {
		return
	}

	// Use taskkill to close Notepad gracefully first, then force if needed
	cmd := exec.Command("taskkill", "/PID", fmt.Sprintf("%d", pid))
	_ = cmd.Run() // Ignore errors, Notepad might have already closed

	// Wait a bit and force kill if still running
	time.Sleep(200 * time.Millisecond)
	cmd = exec.Command("taskkill", "/F", "/PID", fmt.Sprintf("%d", pid))
	_ = cmd.Run() // Ignore errors
}
