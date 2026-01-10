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

	if err := c.cfg.AddService(svc); err != nil {
		return fmt.Errorf("failed to add service: %w", err)
	}

	if verbose {
		fmt.Printf("Service '%s' added to configuration.\n", flags.Name)
	}

	platformMgr, err := platform.NewManager()
	if err != nil {
		_ = c.cfg.RemoveService(flags.Name)
		return fmt.Errorf("failed to create platform manager: %w", err)
	}

	if err := platformMgr.Install(svc); err != nil {
		if strings.Contains(err.Error(), "elevation requested") {
			return err
		}
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
	fmt.Printf("\nTotal: %d service(s)\n", len(services))

	return nil
}

// Remove removes a service completely (from system, config, and scripts).
func (c *CLI) Remove(ctx context.Context, name string, verbose bool) error {
	svc, err := c.cfg.GetService(name)
	if err != nil {
		return fmt.Errorf("service '%s' does not exist", name)
	}

	platformMgr, err := platform.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create platform manager: %w", err)
	}

	if err := platformMgr.Uninstall(name); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to uninstall service from system: %v\n", err)
	}

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

	if svc.Command == scriptPath || strings.HasSuffix(svc.Command, scriptPath) {
		if err := os.Remove(scriptPath); err != nil && !os.IsNotExist(err) {
			if verbose {
				fmt.Fprintf(os.Stderr, "Warning: failed to remove script file: %v\n", err)
			}
		}
	}

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


// Enable enables a service.
func (c *CLI) Enable(ctx context.Context, name string, verbose bool) error {
	svc, err := c.cfg.GetService(name)
	if err != nil {
		return fmt.Errorf("service '%s' does not exist", name)
	}

	if svc.Enabled {
		fmt.Printf("Service '%s' is already enabled.\n", name)
		return nil
	}

	svc.Enabled = true
	if err := c.cfg.UpdateService(svc); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}

	platformMgr, err := platform.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create platform manager: %w", err)
	}

	if err := platformMgr.Install(svc); err != nil {
		return fmt.Errorf("failed to reinstall service: %w", err)
	}

	fmt.Printf("Service '%s' enabled successfully!\n", name)
	return nil
}

// Disable disables a service.
func (c *CLI) Disable(ctx context.Context, name string, verbose bool) error {
	svc, err := c.cfg.GetService(name)
	if err != nil {
		return fmt.Errorf("service '%s' does not exist", name)
	}

	if !svc.Enabled {
		fmt.Printf("Service '%s' is already disabled.\n", name)
		return nil
	}

	svc.Enabled = false
	if err := c.cfg.UpdateService(svc); err != nil {
		return fmt.Errorf("failed to disable service: %w", err)
	}

	platformMgr, err := platform.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create platform manager: %w", err)
	}

	if err := platformMgr.Uninstall(name); err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "Warning: failed to uninstall service: %v\n", err)
		}
	}

	fmt.Printf("Service '%s' disabled successfully!\n", name)
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
		closeNotepad(notepadPID)
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

	cmd := exec.Command("taskkill", "/PID", fmt.Sprintf("%d", pid))
	_ = cmd.Run()

	time.Sleep(200 * time.Millisecond)
	cmd = exec.Command("taskkill", "/F", "/PID", fmt.Sprintf("%d", pid))
	_ = cmd.Run()
}
