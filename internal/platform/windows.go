//go:build windows
// +build windows

// Package platform provides Windows-specific service management.
package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"nazim/internal/service"
)

// WindowsManager manages services on Windows using Task Scheduler.
type WindowsManager struct{}

// NewWindowsManager creates a new manager for Windows.
func NewWindowsManager() *WindowsManager {
	return &WindowsManager{}
}

func isAdmin() bool {
	if runtime.GOOS != "windows" {
		return false
	}

	token := windows.GetCurrentProcessToken()
	elevated := token.IsElevated()
	
	return elevated
}

func requestElevation() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("elevation is only supported on Windows")
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}

	// Properly marshal arguments using Windows escaping rules
	args := marshalWindowsArgs(os.Args[1:])
	if args == "" {
		args = "add"
	}

	verbPtr, err := syscall.UTF16PtrFromString("runas")
	if err != nil {
		return fmt.Errorf("failed to convert verb to UTF16: %w", err)
	}

	exePtr, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		return fmt.Errorf("failed to convert executable path to UTF16: %w", err)
	}

	var argPtr *uint16
	if args != "" {
		argPtr, err = syscall.UTF16PtrFromString(args)
		if err != nil {
			return fmt.Errorf("failed to convert arguments to UTF16: %w", err)
		}
	}

	var cwdPtr *uint16
	if cwd != "" {
		cwdPtr, err = syscall.UTF16PtrFromString(cwd)
		if err != nil {
			return fmt.Errorf("failed to convert working directory to UTF16: %w", err)
		}
	}

	var showCmd int32 = 0

	err = windows.ShellExecute(0, verbPtr, exePtr, argPtr, cwdPtr, showCmd)
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok {
			if errno == 1223 {
				return fmt.Errorf("elevation was cancelled - please approve the UAC prompt to continue")
			}
		}
		return fmt.Errorf("failed to elevate privileges: %w", err)
	}

	return nil
}

func checkAdminOrElevate() error {
	if isAdmin() {
		return nil
	}

	fmt.Fprintf(os.Stderr, "Administrator privileges required for installing services.\n")
	fmt.Fprintf(os.Stderr, "Requesting elevation (UAC prompt will appear)...\n")
	return requestElevation()
}

// Install installs a service on Windows using schtasks.
// On non-admin execution, this triggers UAC elevation and exits the parent process.
// The elevated child process will complete the installation.
func (m *WindowsManager) Install(svc *service.Service) error {
	if !isAdmin() {
		if err := requestElevation(); err != nil {
			if strings.Contains(err.Error(), "cancelled") || strings.Contains(err.Error(), "denied") {
				return fmt.Errorf("UAC prompt was cancelled or denied. Please approve the UAC prompt to install the service")
			}
			return fmt.Errorf("failed to request elevation: %w\nHint: Try running the command as administrator manually", err)
		}
		// Elevation triggered successfully - parent process should exit
		// The elevated child process will handle the actual installation
		return nil
	}

	if err := m.Uninstall(svc.Name); err != nil {
		// Log but don't fail if task doesn't exist
		if !strings.Contains(err.Error(), "does not exist") {
			// Non-critical error, continue
		}
	}

	// Create log directory if it doesn't exist
	logDir := filepath.Join(os.Getenv("APPDATA"), "nazim", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	normalizedName := normalizeServiceName(svc.Name)
	logPath := filepath.Join(logDir, fmt.Sprintf("%s.log", normalizedName))

	// Build the actual command to execute
	command := buildWindowsCommand(svc.Command, svc.Args)
	if svc.WorkDir != "" {
		// Prefix with cd command if WorkDir is specified
		command = fmt.Sprintf(`cd /d %s && %s`, escapeWindowsPath(svc.WorkDir), command)
	}

	// Create logging wrapper that adds timestamps
	wrapperPath, err := createLoggingWrapper(normalizedName, command, logPath)
	if err != nil {
		return fmt.Errorf("failed to create logging wrapper: %w", err)
	}

	hasInterval := svc.GetInterval() > 0
	hasStartup := svc.OnStartup

	// Use PowerShell wrapper for timestamp logging
	commandWithLogs := fmt.Sprintf(`powershell -NoProfile -ExecutionPolicy Bypass -File "%s"`, wrapperPath)

	args := []string{
		"/create",
		"/tn", fmt.Sprintf("Nazim_%s", normalizedName),
		"/tr", commandWithLogs,
		"/f",
	}

	if hasInterval {
		minutes := int(svc.GetInterval().Minutes())
		if minutes > 0 {
			if hasStartup {
				args = append(args, "/sc", "onstart")
				args = append(args, "/ri", fmt.Sprintf("%d", minutes))
			} else {
				args = append(args, "/sc", "minute", "/mo", fmt.Sprintf("%d", minutes))
			}
		}
	} else if hasStartup {
		args = append(args, "/sc", "onstart")
	}

	cmd := exec.Command("schtasks", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create task: %s: %w", string(output), err)
	}

	return nil
}

func buildWindowsCommand(cmd string, args []string) string {
	escapedCmd := escapeWindowsCommand(cmd)
	
	if len(args) == 0 {
		return escapedCmd
	}

	escapedArgs := make([]string, len(args))
	for i, arg := range args {
		escapedArgs[i] = escapeWindowsCommand(arg)
	}
	
	return escapedCmd + " " + strings.Join(escapedArgs, " ")
}

func escapeWindowsCommand(s string) string {
	if strings.ContainsAny(s, " \t\"&|<>()") {
		escaped := strings.ReplaceAll(s, `"`, `""`)
		return `"` + escaped + `"`
	}
	return s
}

func escapeWindowsPath(path string) string {
	escaped := strings.ReplaceAll(path, `"`, `""`)
	return `"` + escaped + `"`
}

// escapePowerShellLiteral escapes a string for use in PowerShell single-quoted (literal) strings.
// Single-quoted strings in PowerShell are literal except for single quotes themselves.
func escapePowerShellLiteral(s string) string {
	// Escape single quotes by doubling them
	escaped := strings.ReplaceAll(s, "'", "''")
	return "'" + escaped + "'"
}

// marshalWindowsArgs properly marshals command-line arguments using Windows escaping rules.
// This implements the CommandLineToArgvW escaping algorithm.
func marshalWindowsArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}

	var quoted []string
	for _, arg := range args {
		quoted = append(quoted, quoteWindowsArg(arg))
	}
	return strings.Join(quoted, " ")
}

// quoteWindowsArg quotes a single argument according to Windows CommandLineToArgvW rules.
func quoteWindowsArg(arg string) string {
	// Check if quoting is needed
	if arg == "" {
		return `""`
	}

	needsQuote := strings.ContainsAny(arg, " \t\"")
	if !needsQuote {
		return arg
	}

	// Build escaped argument
	var result strings.Builder
	result.WriteByte('"')

	for i := 0; i < len(arg); i++ {
		backslashes := 0

		// Count consecutive backslashes
		for i < len(arg) && arg[i] == '\\' {
			backslashes++
			i++
		}

		if i >= len(arg) {
			// Backslashes at end - double them
			for j := 0; j < backslashes*2; j++ {
				result.WriteByte('\\')
			}
			break
		} else if arg[i] == '"' {
			// Backslashes before quote - double them + escape quote
			for j := 0; j < backslashes*2+1; j++ {
				result.WriteByte('\\')
			}
			result.WriteByte('"')
		} else {
			// Normal backslashes
			for j := 0; j < backslashes; j++ {
				result.WriteByte('\\')
			}
			result.WriteByte(arg[i])
		}
	}

	result.WriteByte('"')
	return result.String()
}

// createLoggingWrapper creates a PowerShell wrapper script that adds timestamps to logs.
// Returns the wrapper path and an error if creation fails.
func createLoggingWrapper(normalizedName, command, logPath string) (string, error) {
	// Save wrapper script in dedicated wrappers directory
	wrapperDir := filepath.Join(os.Getenv("APPDATA"), "nazim", "wrappers")
	if err := os.MkdirAll(wrapperDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create wrappers directory: %w", err)
	}

	wrapperPath := filepath.Join(wrapperDir, fmt.Sprintf("%s-wrapper.ps1", normalizedName))

	// Escape command for PowerShell (use single quotes for literal strings)
	escapedCommand := strings.ReplaceAll(command, "'", "''")
	escapedLogPath := strings.ReplaceAll(logPath, "'", "''")

	// Create PowerShell wrapper with timestamp logging
	wrapperContent := fmt.Sprintf(`# Nazim Logging Wrapper
# Service: %s
# Auto-generated - Do not edit manually

$ErrorActionPreference = "Continue"

function Get-Timestamp {
    $local = Get-Date -Format "yyyy-MM-dd HH:mm:ss K"
    $utc = (Get-Date).ToUniversalTime().ToString("yyyy-MM-dd HH:mm:ss")
    return "[$local / $utc UTC]"
}

$logFile = '%s'

# Ensure log directory exists
$logDir = Split-Path -Parent $logFile
if (-not (Test-Path $logDir)) {
    New-Item -ItemType Directory -Path $logDir -Force | Out-Null
}

# Log start
$timestamp = Get-Timestamp
Add-Content -Path $logFile -Value "$timestamp Starting execution"

# Execute command and capture all output
try {
    $output = & cmd /c '%s' 2>&1
    $exitCode = $LASTEXITCODE

    # Log each line of output with timestamp
    if ($output) {
        $timestamp = Get-Timestamp
        if ($output -is [array]) {
            foreach ($line in $output) {
                Add-Content -Path $logFile -Value "$timestamp $line"
            }
        } else {
            Add-Content -Path $logFile -Value "$timestamp $output"
        }
    }
} catch {
    $timestamp = Get-Timestamp
    Add-Content -Path $logFile -Value "$timestamp ERROR: $_"
    $exitCode = 1
}

# Log finish
$timestamp = Get-Timestamp
Add-Content -Path $logFile -Value "$timestamp Finished with exit code $exitCode"
Add-Content -Path $logFile -Value ""

exit $exitCode
`, normalizedName, escapedLogPath, escapedCommand)

	if err := os.WriteFile(wrapperPath, []byte(wrapperContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write wrapper script: %w", err)
	}

	return wrapperPath, nil
}

// deleteLoggingWrapper removes the wrapper script for a service.
func deleteLoggingWrapper(normalizedName string) {
	wrapperPath := filepath.Join(os.Getenv("APPDATA"), "nazim", "wrappers", fmt.Sprintf("%s-wrapper.ps1", normalizedName))
	_ = os.Remove(wrapperPath)
}

// Uninstall removes a service from Windows.
// On non-admin execution, this triggers UAC elevation and returns a special error.
// The CLI layer detects this error and exits silently, allowing the elevated process to complete.
func (m *WindowsManager) Uninstall(name string) error {
	normalizedName := normalizeServiceName(name)
	taskName := fmt.Sprintf("Nazim_%s", normalizedName)

	// Require admin upfront for task deletion
	if !isAdmin() {
		if err := checkAdminOrElevate(); err != nil {
			return fmt.Errorf("administrator privileges required: %w", err)
		}
		// Elevated process will handle deletion - return error to stop original process
		return fmt.Errorf("Elevated process will handle deletion")
	}

	// We are admin - proceed with deletion
	cmd := exec.Command("schtasks", "/delete", "/tn", taskName, "/f")
	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := strings.ToLower(string(output))
		// Task doesn't exist = success
		if strings.Contains(outputStr, "does not exist") ||
			strings.Contains(outputStr, "cannot find") ||
			strings.Contains(outputStr, "not found") {
			// Still delete wrapper even if task doesn't exist
			deleteLoggingWrapper(normalizedName)
			return nil
		}
		return fmt.Errorf("failed to delete task: %s: %w", string(output), err)
	}

	// Delete the logging wrapper script
	deleteLoggingWrapper(normalizedName)

	return nil
}

// Enable enables a service on Windows (allows it to run on schedule).
// On non-admin execution, this triggers UAC elevation and returns a special error.
// The CLI layer detects this error and exits silently, allowing the elevated process to complete.
func (m *WindowsManager) Enable(name string) error {
	normalizedName := normalizeServiceName(name)
	taskName := fmt.Sprintf("Nazim_%s", normalizedName)

	// Require admin upfront for task modification
	if !isAdmin() {
		if err := checkAdminOrElevate(); err != nil {
			return fmt.Errorf("administrator privileges required: %w", err)
		}
		// Elevated process will handle enabling - return error to stop original process
		return fmt.Errorf("Elevated process will handle enable")
	}

	// Enable the task so it will run on schedule
	cmd := exec.Command("schtasks", "/change", "/tn", taskName, "/enable")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to enable task: %s: %w", string(output), err)
	}
	return nil
}

// Disable disables a service on Windows (prevents it from running on schedule).
// On non-admin execution, this triggers UAC elevation and returns a special error.
// The CLI layer detects this error and exits silently, allowing the elevated process to complete.
func (m *WindowsManager) Disable(name string) error {
	normalizedName := normalizeServiceName(name)
	taskName := fmt.Sprintf("Nazim_%s", normalizedName)

	// Require admin upfront for task modification
	if !isAdmin() {
		if err := checkAdminOrElevate(); err != nil {
			return fmt.Errorf("administrator privileges required: %w", err)
		}
		// Elevated process will handle disabling - return error to stop original process
		return fmt.Errorf("Elevated process will handle disable")
	}

	// Disable the task to stop future scheduled executions
	cmd := exec.Command("schtasks", "/change", "/tn", taskName, "/disable")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to disable task: %s: %w", string(output), err)
	}
	return nil
}

// Run executes a service immediately (independent of schedule).
// This triggers immediate execution regardless of the task's enabled/disabled state.
func (m *WindowsManager) Run(name string) error {
	normalizedName := normalizeServiceName(name)
	taskName := fmt.Sprintf("Nazim_%s", normalizedName)

	// /run doesn't require admin privileges on already-created tasks
	// It just triggers execution using the existing task definition
	cmd := exec.Command("schtasks", "/run", "/tn", taskName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := strings.ToLower(string(output))
		// Provide helpful error messages
		if strings.Contains(outputStr, "does not exist") ||
			strings.Contains(outputStr, "cannot find") {
			return fmt.Errorf("task not found - service may not be installed")
		}
		return fmt.Errorf("failed to run task: %s: %w", string(output), err)
	}

	return nil
}

// IsInstalled checks if a service is installed.
func (m *WindowsManager) IsInstalled(name string) (bool, error) {
	normalizedName := normalizeServiceName(name)
	taskName := fmt.Sprintf("Nazim_%s", normalizedName)

	cmd := exec.Command("schtasks", "/query", "/tn", taskName)
	err := cmd.Run()
	return err == nil, nil
}

// GetTaskState returns the state of a scheduled task ("Enabled" or "Disabled").
// Returns error if task is not found.
func (m *WindowsManager) GetTaskState(name string) (string, error) {
	normalizedName := normalizeServiceName(name)
	taskName := fmt.Sprintf("Nazim_%s", normalizedName)

	cmd := exec.Command("schtasks", "/query", "/tn", taskName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := strings.ToLower(string(output))
		if strings.Contains(outputStr, "does not exist") ||
			strings.Contains(outputStr, "cannot find") {
			return "", fmt.Errorf("task not found")
		}
		return "", fmt.Errorf("failed to query task: %w", err)
	}

	outputStr := string(output)

	// Check if task is disabled by looking for "Disabled" in the Status column
	if strings.Contains(outputStr, "Disabled") {
		return "Disabled", nil
	}

	// If not disabled and task exists, it's enabled (Ready, Running, etc.)
	return "Enabled", nil
}
