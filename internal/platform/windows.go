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

	args := strings.Join(os.Args[1:], " ")

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

	var showCmd int32 = 1

	err = windows.ShellExecute(0, verbPtr, exePtr, argPtr, cwdPtr, showCmd)
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok {
			if errno == 1223 {
				return fmt.Errorf("elevation was cancelled - please approve the UAC prompt to continue")
			}
		}
		return fmt.Errorf("failed to elevate privileges: %w", err)
	}

	os.Exit(0)
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
func (m *WindowsManager) Install(svc *service.Service) error {
	if !isAdmin() {
		fmt.Fprintf(os.Stderr, "\n[!] Administrator privileges required for installing services.\n")
		fmt.Fprintf(os.Stderr, "[!] Requesting elevation (UAC prompt will appear)...\n")
		fmt.Fprintf(os.Stderr, "[!] If UAC prompt does not appear, please run the command as administrator.\n\n")

		if err := requestElevation(); err != nil {
			if strings.Contains(err.Error(), "cancelled") || strings.Contains(err.Error(), "denied") {
				return fmt.Errorf("UAC prompt was cancelled or denied. Please approve the UAC prompt to install the service")
			}
			return fmt.Errorf("failed to request elevation: %w\nHint: Try running the command as administrator manually", err)
		}
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

	command := buildWindowsCommand(svc.Command, svc.Args)

	normalizedNameForLog := normalizeServiceName(svc.Name)
	logPath := filepath.Join(logDir, fmt.Sprintf("%s.log", normalizedNameForLog))
	escapedLogPath := escapeWindowsPath(logPath)
	commandWithLogs := fmt.Sprintf(`cmd /c "%s >> %s 2>&1"`, command, escapedLogPath)

	normalizedName := normalizeServiceName(svc.Name)

	args := []string{
		"/create",
		"/tn", fmt.Sprintf("Nazim_%s", normalizedName),
		"/tr", commandWithLogs,
		"/f",
	}

	if svc.WorkDir != "" {
		wrappedCommand := fmt.Sprintf(`cmd /c "cd /d %s && %s >> %s 2>&1"`, escapeWindowsPath(svc.WorkDir), command, escapedLogPath)
		args[3] = wrappedCommand
	}

	hasInterval := svc.GetInterval() > 0
	hasStartup := svc.OnStartup

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
	} else {
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

// Uninstall removes a service from Windows.
func (m *WindowsManager) Uninstall(name string) error {
	normalizedName := normalizeServiceName(name)
	taskName := fmt.Sprintf("Nazim_%s", normalizedName)

	if !isAdmin() {
		cmd := exec.Command("schtasks", "/delete", "/tn", taskName, "/f")
		output, err := cmd.CombinedOutput()
		if err != nil {
			outputStr := strings.ToLower(string(output))
			if strings.Contains(outputStr, "does not exist") ||
				strings.Contains(outputStr, "cannot find") ||
				strings.Contains(outputStr, "not found") {
				return nil
			}
			if strings.Contains(outputStr, "access is denied") || strings.Contains(outputStr, "privileges") {
				if err := checkAdminOrElevate(); err != nil {
					return fmt.Errorf("administrator privileges required: %w", err)
				}
				return nil
			}
			return fmt.Errorf("failed to delete task: %s: %w", string(output), err)
		}
		return nil
	}

	cmd := exec.Command("schtasks", "/delete", "/tn", taskName, "/f")
		output, err := cmd.CombinedOutput()
		if err != nil {
			outputStr := strings.ToLower(string(output))
			if strings.Contains(outputStr, "does not exist") ||
			strings.Contains(outputStr, "cannot find") ||
				strings.Contains(outputStr, "not found") {
				return nil
			}
		return fmt.Errorf("failed to delete task: %s: %w", string(output), err)
	}
	return nil
}

// Start starts a service on Windows.
func (m *WindowsManager) Start(name string) error {
	normalizedName := normalizeServiceName(name)
	taskName := fmt.Sprintf("Nazim_%s", normalizedName)

	cmd := exec.Command("schtasks", "/run", "/tn", taskName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start task: %s: %w", string(output), err)
	}
	return nil
}

// Stop stops a service on Windows (terminates the running task).
func (m *WindowsManager) Stop(name string) error {
	normalizedName := normalizeServiceName(name)
	taskName := fmt.Sprintf("Nazim_%s", normalizedName)

	cmd := exec.Command("schtasks", "/end", "/tn", taskName)
		output, err := cmd.CombinedOutput()
		if err != nil {
			outputStr := strings.ToLower(string(output))
			if strings.Contains(outputStr, "is not running") {
				return nil
			}
		return fmt.Errorf("failed to stop task: %s: %w", string(output), err)
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
