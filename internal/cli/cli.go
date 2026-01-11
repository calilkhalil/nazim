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
	if flags.Name == "" {
		return fmt.Errorf("service name is required (use --name or -n)")
	}
	if flags.Command == "" {
		return fmt.Errorf("service command is required (use --command or -c, or 'write'/'edit' for interactive mode)")
	}

	command := flags.Command
	if command == "write" || command == "edit" {
		scriptPath, err := c.createScriptInteractive(flags.Name, verbose)
		if err != nil {
			return fmt.Errorf("failed to create script: %w", err)
		}
		command = scriptPath
	}

	var intervalDuration time.Duration
	if flags.Interval != "" {
		var err error
		intervalDuration, err = parseDuration(flags.Interval)
		if err != nil {
			return fmt.Errorf("invalid interval format: %w", err)
		}
	}

	var args []string
	if flags.Args != "" {
		args = strings.Fields(flags.Args)
	}

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

	if err := svc.Validate(); err != nil {
		return err
	}

	platformMgr, err := platform.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create platform manager: %w", err)
	}

	if err := platformMgr.Install(svc); err != nil {
		return fmt.Errorf("failed to install service: %w", err)
	}

	if err := c.cfg.AddService(svc); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			if err := c.cfg.UpdateService(svc); err != nil {
				if uninstallErr := platformMgr.Uninstall(svc.Name); uninstallErr != nil {
					return fmt.Errorf("failed to update service in config: %w (also failed to uninstall: %v)", err, uninstallErr)
				}
				return fmt.Errorf("failed to update service in config: %w", err)
			}
		} else {
			if uninstallErr := platformMgr.Uninstall(svc.Name); uninstallErr != nil {
				return fmt.Errorf("failed to add service to config: %w (also failed to uninstall: %v)", err, uninstallErr)
			}
			return fmt.Errorf("failed to add service to config: %w", err)
		}
	}

	if verbose {
		fmt.Printf("Service '%s' added to configuration.\n", flags.Name)
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

	type rowData struct {
		name    string
		command string
		svcType string
		status  string
	}

	rows := make([]rowData, 0, len(services))
	for _, svc := range services {
		// Get task state (Enabled/Disabled)
		state, err := platformMgr.GetTaskState(svc.Name)
		status := state
		if err != nil {
			// If task not found, skip this service (it shouldn't be in the list)
			if verbose {
				fmt.Fprintf(os.Stderr, "Warning: service '%s' not found in system, skipping...\n", svc.Name)
			}
			continue
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

	// If no valid services found, show message
	if len(rows) == 0 {
		fmt.Println("No services found.")
		return nil
	}

	const maxCmdDisplay = 50
	for i := range rows {
		if len(rows[i].command) > maxCmdDisplay {
			rows[i].command = rows[i].command[:maxCmdDisplay] + "..."
		}
	}

	maxNameLen := 4
	maxCmdLen := 7
	maxTypeLen := 4
	maxStatusLen := 6

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

	headerSeparator := strings.Repeat("-", maxNameLen+maxCmdLen+maxTypeLen+maxStatusLen+13)
	fmt.Println(headerSeparator)
	fmt.Printf("| %-*s | %-*s | %-*s | %-*s |\n",
		maxNameLen, "NAME",
		maxCmdLen, "COMMAND",
		maxTypeLen, "TYPE",
		maxStatusLen, "STATUS")
	fmt.Println(headerSeparator)

	for _, row := range rows {
		fmt.Printf("| %-*s | %-*s | %-*s | %-*s |\n",
			maxNameLen, row.name,
			maxCmdLen, row.command,
			maxTypeLen, row.svcType,
			maxStatusLen, row.status)
	}

	fmt.Println(headerSeparator)
	fmt.Printf("\nTotal: %d service(s)\n", len(rows))

	return nil
}

// Remove removes a service completely (from system, config, scripts, and logs).
func (c *CLI) Remove(ctx context.Context, name string, verbose bool) error {
	if _, err := c.cfg.GetService(name); err != nil {
		return fmt.Errorf("service '%s' does not exist", name)
	}

	var removalErrors []string

	// 1. Remove from platform (Task Scheduler, systemd, launchd)
	platformMgr, err := platform.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create platform manager: %w", err)
	}

	if err := platformMgr.Uninstall(name); err != nil {
		// Check if elevation was triggered
		if strings.Contains(err.Error(), "Elevated process will handle") {
			// Elevation was triggered - stop here, elevated process will handle everything
			// Don't show error, just exit silently (elevated process will show success)
			return nil
		}
		// Other errors are warnings
		if verbose {
			fmt.Fprintf(os.Stderr, "Warning: failed to uninstall from system: %v\n", err)
		}
		removalErrors = append(removalErrors, fmt.Sprintf("system uninstall: %v", err))
	}

	// 2. Remove script file (always attempt, regardless of svc.Command)
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

	if err := os.Remove(scriptPath); err != nil && !os.IsNotExist(err) {
		if verbose {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove script file: %v\n", err)
		}
		removalErrors = append(removalErrors, fmt.Sprintf("script file: %v", err))
	} else if err == nil {
		if verbose {
			fmt.Printf("Removed script file: %s\n", scriptPath)
		}
	}

	// 3. Remove log file
	logsDir := c.cfg.GetLogsDir()
	logPath := filepath.Join(logsDir, fmt.Sprintf("%s.log", name))

	if err := os.Remove(logPath); err != nil && !os.IsNotExist(err) {
		if verbose {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove log file: %v\n", err)
		}
		removalErrors = append(removalErrors, fmt.Sprintf("log file: %v", err))
	} else if err == nil {
		if verbose {
			fmt.Printf("Removed log file: %s\n", logPath)
		}
	}

	// 4. Log removal with timestamp
	removalLogPath := filepath.Join(logsDir, "removals.log")
	logEntry := fmt.Sprintf("%s - Service '%s' removed\n",
		time.Now().Format("2006-01-02 15:04:05"), name)

	f, err := os.OpenFile(removalLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "Warning: failed to log removal: %v\n", err)
		}
	} else {
		defer f.Close()
		if _, err := f.WriteString(logEntry); err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "Warning: failed to write removal log: %v\n", err)
			}
		}
	}

	// 5. Remove from config
	if err := c.cfg.RemoveService(name); err != nil {
		return fmt.Errorf("failed to remove service from config: %w", err)
	}

	// Report results
	if len(removalErrors) > 0 {
		fmt.Printf("Service '%s' removed from config with warnings:\n", name)
		for _, e := range removalErrors {
			fmt.Printf("  - %s\n", e)
		}
	} else {
		fmt.Printf("Service '%s' removed successfully!\n", name)
	}

	return nil
}

// Enable enables a service (allows it to run on schedule).
func (c *CLI) Enable(ctx context.Context, name string, verbose bool) error {
	if _, err := c.cfg.GetService(name); err != nil {
		return fmt.Errorf("service '%s' does not exist", name)
	}

	platformMgr, err := platform.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create platform manager: %w", err)
	}

	if err := platformMgr.Enable(name); err != nil {
		// Check if elevated process is handling it
		if strings.Contains(err.Error(), "Elevated process will handle") {
			// UAC was triggered, elevated process will show success message
			return nil
		}
		return fmt.Errorf("failed to enable service: %w", err)
	}

	fmt.Printf("Service '%s' enabled successfully!\n", name)
	return nil
}

// Disable disables a service (prevents it from running on schedule).
func (c *CLI) Disable(ctx context.Context, name string, verbose bool) error {
	if _, err := c.cfg.GetService(name); err != nil {
		return fmt.Errorf("service '%s' does not exist", name)
	}

	platformMgr, err := platform.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create platform manager: %w", err)
	}

	if err := platformMgr.Disable(name); err != nil {
		// Check if elevated process is handling it
		if strings.Contains(err.Error(), "Elevated process will handle") {
			// UAC was triggered, elevated process will show success message
			return nil
		}
		return fmt.Errorf("failed to disable service: %w", err)
	}

	fmt.Printf("Service '%s' disabled successfully!\n", name)
	return nil
}

// Run executes a service immediately (independent of schedule).
func (c *CLI) Run(ctx context.Context, name string, verbose bool) error {
	if _, err := c.cfg.GetService(name); err != nil {
		return fmt.Errorf("service '%s' does not exist", name)
	}

	platformMgr, err := platform.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create platform manager: %w", err)
	}

	if err := platformMgr.Run(name); err != nil {
		return fmt.Errorf("failed to run service: %w", err)
	}

	fmt.Printf("Service '%s' is now running!\n", name)
	return nil
}

// Status shows detailed information about a service.
func (c *CLI) Status(ctx context.Context, name string, verbose bool) error {
	svc, err := c.cfg.GetService(name)
	if err != nil {
		return fmt.Errorf("service '%s' does not exist", name)
	}

	platformMgr, err := platform.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create platform manager: %w", err)
	}

	installed, err := platformMgr.IsInstalled(name)
	status := "Not Installed"
	if installed {
		status = "Installed"
	} else if err != nil {
		status = "Unknown"
	}

	fmt.Printf("Service: %s\n", svc.Name)
	fmt.Printf("Status: %s\n", status)
	fmt.Printf("Enabled: %v\n", svc.Enabled)
	fmt.Printf("Command: %s", svc.Command)
	if len(svc.Args) > 0 {
		fmt.Printf(" %s", strings.Join(svc.Args, " "))
	}
	fmt.Println()
	if svc.WorkDir != "" {
		fmt.Printf("Working Directory: %s\n", svc.WorkDir)
	}
	fmt.Printf("Platform: %s\n", svc.Platform)
	
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
		svcType = "None"
	}
	fmt.Printf("Schedule: %s\n", svcType)

	return nil
}

// Edit updates an existing service.
func (c *CLI) Edit(ctx context.Context, name string, flags *Flags, verbose bool) error {
	existingSvc, err := c.cfg.GetService(name)
	if err != nil {
		return fmt.Errorf("service '%s' does not exist", name)
	}

	var intervalDuration time.Duration
	if flags.Interval != "" {
		var err error
		intervalDuration, err = parseDuration(flags.Interval)
		if err != nil {
			return fmt.Errorf("invalid interval format: %w", err)
		}
	} else {
		intervalDuration = existingSvc.GetInterval()
	}

	var args []string
	if flags.Args != "" {
		args = strings.Fields(flags.Args)
	} else {
		args = existingSvc.Args
	}

	updatedSvc := &service.Service{
		Name:      name,
		Command:   existingSvc.Command,
		Args:      existingSvc.Args,
		WorkDir:   existingSvc.WorkDir,
		OnStartup: existingSvc.OnStartup,
		Interval:  existingSvc.Interval,
		Enabled:   existingSvc.Enabled,
		Platform:  existingSvc.Platform,
	}

	if flags.Command != "" {
		updatedSvc.Command = flags.Command
	}
	if flags.Args != "" {
		updatedSvc.Args = args
	}
	if flags.WorkDir != "" {
		updatedSvc.WorkDir = flags.WorkDir
	}
	if flags.Interval != "" {
		updatedSvc.Interval = service.Duration{Duration: intervalDuration}
	}

	if flags.OnStartup {
		updatedSvc.OnStartup = true
		updatedSvc.Interval = service.Duration{Duration: 0}
	} else if flags.Interval != "" {
		updatedSvc.OnStartup = false
	}

	if err := updatedSvc.Validate(); err != nil {
		return err
	}

	if err := c.cfg.UpdateService(updatedSvc); err != nil {
		return fmt.Errorf("failed to update service: %w", err)
	}

	if verbose {
		fmt.Printf("Service '%s' updated in configuration.\n", name)
	}

	platformMgr, err := platform.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create platform manager: %w", err)
	}

	if err := platformMgr.Uninstall(name); err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "Warning: failed to uninstall old service: %v\n", err)
		}
	}

	if err := platformMgr.Install(updatedSvc); err != nil {
		return fmt.Errorf("failed to reinstall service: %w", err)
	}

	fmt.Printf("Service '%s' updated successfully!\n", name)
	return nil
}


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

	if value <= 0 {
		return 0, fmt.Errorf("duration value must be positive, got %d", value)
	}

	return time.Duration(value) * multiplier, nil
}

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

func (c *CLI) createScriptInteractive(serviceName string, verbose bool) (string, error) {
	scriptsDir := c.cfg.GetScriptsDir()
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		return "", fmt.Errorf("creating scripts directory: %w", err)
	}

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

	// Check if script already exists from previous execution (e.g., after UAC elevation)
	if _, err := os.Stat(scriptPath); err == nil {
		// File exists, use it without opening editor again
		if verbose {
			fmt.Printf("Script already exists at %s, using it\n", scriptPath)
		}
		return scriptPath, nil
	}

	editor := getEditor()

	if verbose {
		fmt.Printf("Opening editor: %s\n", editor)
		fmt.Printf("Script will be saved to: %s\n", scriptPath)
	}

	var initialContent string
	switch runtime.GOOS {
	case "windows":
		initialContent = createWindowsTemplate(serviceName)
	case "darwin", "linux":
		initialContent = createUnixTemplate(serviceName)
	default:
		initialContent = createUnixTemplate(serviceName)
	}

	if err := os.WriteFile(scriptPath, []byte(initialContent), 0644); err != nil {
		return "", fmt.Errorf("creating script file: %w", err)
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(scriptPath, 0755); err != nil {
			return "", fmt.Errorf("making script executable: %w", err)
		}
	}

	if runtime.GOOS == "windows" && editor == "notepad" {
		return c.openNotepadWithMonitoring(scriptPath, verbose)
	}

	var cmd *exec.Cmd
	if editor == "open" && runtime.GOOS == "darwin" {
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

func getEditor() string {
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}

	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor
	}

	switch runtime.GOOS {
	case "windows":
		if _, err := exec.LookPath("code"); err == nil {
			return "code"
		}
		if _, err := exec.LookPath("notepad"); err == nil {
			return "notepad"
		}
		return "notepad"
	case "darwin":
		if _, err := exec.LookPath("code"); err == nil {
			return "code"
		}
		if _, err := exec.LookPath("vim"); err == nil {
			return "vim"
		}
		if _, err := exec.LookPath("nano"); err == nil {
			return "nano"
		}
		if _, err := exec.LookPath("open"); err == nil {
			return "open"
		}
		return "vim"
	case "linux":
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
		return "nano"
	default:
		return "vi"
	}
}

func createUnixTemplate(serviceName string) string {
	return fmt.Sprintf(`#!/bin/sh
# Service: %s
# Created: %s

# Your code here:

	exit 0
`, serviceName, time.Now().Format("2006-01-02 15:04:05"))
}

func createWindowsTemplate(serviceName string) string {
	username := os.Getenv("USERNAME")
	if username == "" {
		username = os.Getenv("USER")
	}
	if username == "" {
		username = "unknown"
	}

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

func (c *CLI) openNotepadWithMonitoring(scriptPath string, verbose bool) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	initialInfo, err := os.Stat(scriptPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}
	initialModTime := initialInfo.ModTime()

	cmd := exec.CommandContext(ctx, "notepad", scriptPath)
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start notepad: %w", err)
	}

	notepadPID := cmd.Process.Pid

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
				if runtime.GOOS == "windows" {
					checkCmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", notepadPID), "/NH")
					output, _ := checkCmd.Output()
					if !strings.Contains(strings.ToLower(string(output)), fmt.Sprintf("%d", notepadPID)) {
						fileSaved <- false
						return
					}
				} else {
					if err := cmd.Process.Signal(os.Signal(syscall.Signal(0))); err != nil {
						fileSaved <- false
						return
					}
				}

				info, err := os.Stat(scriptPath)
				if err == nil && !info.ModTime().Equal(initialModTime) {
					time.Sleep(500 * time.Millisecond)
					fileSaved <- true
					return
				}
			}
		}
	}()

	saved := false
	select {
	case result := <-fileSaved:
		saved = result
	case <-ctx.Done():
		return "", fmt.Errorf("timeout waiting for file save")
	}

	if saved {
		time.Sleep(300 * time.Millisecond)
		// Use title-based closing for better reliability
		closeNotepadByTitle(scriptPath)
	}

	_, _ = cmd.Process.Wait()

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

func closeNotepad(pid int) {
	if runtime.GOOS != "windows" {
		return
	}

	// Try graceful close first
	cmd := exec.Command("taskkill", "/PID", fmt.Sprintf("%d", pid))
	_ = cmd.Run()

	// Wait for graceful close
	time.Sleep(500 * time.Millisecond)

	// Check if process still exists
	checkCmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH")
	output, _ := checkCmd.Output()

	// If still running, force close
	if strings.Contains(string(output), fmt.Sprintf("%d", pid)) {
		forceCmd := exec.Command("taskkill", "/F", "/PID", fmt.Sprintf("%d", pid))
		_ = forceCmd.Run()
	}
}

// closeNotepadByTitle closes notepad by window title (more reliable than PID)
func closeNotepadByTitle(scriptPath string) {
	if runtime.GOOS != "windows" {
		return
	}

	// Extract filename for window title matching
	filename := filepath.Base(scriptPath)

	// Escape filename for PowerShell (single quotes are safest)
	escapedFilename := strings.ReplaceAll(filename, "'", "''")

	// Try graceful close first using CloseMainWindow()
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		fmt.Sprintf("Get-Process notepad -ErrorAction SilentlyContinue | Where-Object {$_.MainWindowTitle -like '*%s*'} | ForEach-Object {$_.CloseMainWindow()} | Out-Null", escapedFilename))
	_ = cmd.Run()

	// Wait for graceful close
	time.Sleep(500 * time.Millisecond)

	// Force close if still open
	forceCmd := exec.Command("powershell", "-NoProfile", "-Command",
		fmt.Sprintf("Get-Process notepad -ErrorAction SilentlyContinue | Where-Object {$_.MainWindowTitle -like '*%s*'} | Stop-Process -Force", escapedFilename))
	_ = forceCmd.Run()
}
