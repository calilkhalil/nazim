//go:build windows
// +build windows

// Package platform provides Windows-specific service management.
package platform

import (
	"fmt"
	"os"
	"os/exec"
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

// isAdmin checks if the current process is running with administrator privileges.
// Uses windows.GetCurrentProcessToken().IsElevated() which is more reliable than
// trying to query system tasks or open protected files.
func isAdmin() bool {
	if runtime.GOOS != "windows" {
		return false
	}

	// Use the Windows API to check if the current process token is elevated
	// This is more reliable than trying to query tasks or open protected files
	token := windows.GetCurrentProcessToken()
	elevated := token.IsElevated()
	
	return elevated
}

// requestElevation attempts to re-run the current process with administrator privileges.
// Uses Windows ShellExecute API with "runas" verb which directly triggers UAC prompt.
// This is more reliable than using PowerShell.
func requestElevation() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("elevation is only supported on Windows")
	}

	// Get the current executable path
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "" // Use empty string if we can't get CWD
	}

	// Get command line arguments and join them
	args := strings.Join(os.Args[1:], " ")

	// Convert strings to UTF16 pointers for Windows API
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

	// SW_NORMAL = 1 (show window normally)
	var showCmd int32 = 1

	// Use ShellExecute with "runas" verb to trigger UAC prompt
	err = windows.ShellExecute(0, verbPtr, exePtr, argPtr, cwdPtr, showCmd)
	if err != nil {
		// Check for specific error codes
		if errno, ok := err.(syscall.Errno); ok {
			if errno == 1223 {
				// ERROR_CANCELLED - user cancelled UAC prompt
				return fmt.Errorf("elevation was cancelled - please approve the UAC prompt to continue")
			}
		}
		return fmt.Errorf("failed to elevate privileges: %w", err)
	}

	// If elevation was successful, exit this non-elevated process
	// The elevated process will continue execution
	os.Exit(0)
	return nil
}

// checkAdminOrElevate checks if running as admin, and if not, attempts to elevate.
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
	// Check for admin privileges or request elevation
	if !isAdmin() {
		fmt.Fprintf(os.Stderr, "\n[!] Administrator privileges required for installing services.\n")
		fmt.Fprintf(os.Stderr, "[!] Requesting elevation (UAC prompt will appear)...\n")
		fmt.Fprintf(os.Stderr, "[!] If UAC prompt does not appear, please run the command as administrator.\n\n")
		
		if err := requestElevation(); err != nil {
			// Check if the error indicates UAC was denied/cancelled
			if strings.Contains(err.Error(), "cancelled") || strings.Contains(err.Error(), "denied") {
				return fmt.Errorf("UAC prompt was cancelled or denied. Please approve the UAC prompt to install the service")
			}
			return fmt.Errorf("failed to request elevation: %w\nHint: Try running the command as administrator manually", err)
		}
		// If elevation was successful, this process will exit and a new elevated one will run
		// So we return here to avoid continuing in the non-elevated process
		return nil
	}

	// Primeiro, remover se já existir
	_ = m.Uninstall(svc.Name)

	// Construir o comando
	cmdParts := []string{svc.Command}
	cmdParts = append(cmdParts, svc.Args...)
	command := strings.Join(cmdParts, " ")

	// Criar comando schtasks
	args := []string{
		"/create",
		"/tn", fmt.Sprintf("Nazim_%s", svc.Name),
		"/tr", command,
		"/sc", "onstart", // Por padrão, no startup
		"/f", // Forçar criação
	}

	// Se tiver diretório de trabalho, adicionar
	if svc.WorkDir != "" {
		args = append(args, "/ru", "SYSTEM", "/rp", "")
		// Nota: /cwd não é suportado diretamente, precisamos usar um wrapper
	}

	// Se tiver intervalo, usar /sc onlogon ou criar tarefa agendada
	if svc.GetInterval() > 0 {
		// Converter intervalo para formato do Windows Task Scheduler
		// Para intervalos, usamos /sc minute com /mo (modifier)
		minutes := int(svc.GetInterval().Minutes())
		if minutes > 0 {
			args = []string{
				"/create",
				"/tn", fmt.Sprintf("Nazim_%s", svc.Name),
				"/tr", command,
				"/sc", "minute",
				"/mo", fmt.Sprintf("%d", minutes),
				"/f",
			}
		}
	}

	// Se for apenas startup, manter /sc onstart
	if svc.OnStartup && svc.GetInterval() == 0 {
		args = []string{
			"/create",
			"/tn", fmt.Sprintf("Nazim_%s", svc.Name),
			"/tr", command,
			"/sc", "onstart",
			"/f",
		}
	}

	cmd := exec.Command("schtasks", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create task: %s: %w", string(output), err)
	}

	return nil
}

// Uninstall removes a service from Windows.
func (m *WindowsManager) Uninstall(name string) error {
	// Check for admin privileges (needed for system tasks)
	if !isAdmin() {
		// For uninstall, we can try without elevation first
		// If it fails due to permissions, we'll request elevation
		taskName := fmt.Sprintf("Nazim_%s", name)
		cmd := exec.Command("schtasks", "/delete", "/tn", taskName, "/f")
		output, err := cmd.CombinedOutput()
		if err != nil {
			// If error is about permissions, request elevation
			if strings.Contains(string(output), "access is denied") || strings.Contains(string(output), "privileges") {
				if err := checkAdminOrElevate(); err != nil {
					return fmt.Errorf("administrator privileges required: %w", err)
				}
				return nil // Process will exit and restart elevated
			}
			// Ignorar erro se a tarefa não existir
			if !strings.Contains(string(output), "does not exist") {
				return fmt.Errorf("failed to delete task: %s: %w", string(output), err)
			}
		}
		return nil
	}

	taskName := fmt.Sprintf("Nazim_%s", name)
	cmd := exec.Command("schtasks", "/delete", "/tn", taskName, "/f")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Ignorar erro se a tarefa não existir
		if !strings.Contains(string(output), "does not exist") {
			return fmt.Errorf("failed to delete task: %s: %w", string(output), err)
		}
	}
	return nil
}

// Start starts a service on Windows.
func (m *WindowsManager) Start(name string) error {
	taskName := fmt.Sprintf("Nazim_%s", name)
	cmd := exec.Command("schtasks", "/run", "/tn", taskName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start task: %s: %w", string(output), err)
	}
	return nil
}

// Stop stops a service on Windows (terminates the running task).
func (m *WindowsManager) Stop(name string) error {
	taskName := fmt.Sprintf("Nazim_%s", name)
	cmd := exec.Command("schtasks", "/end", "/tn", taskName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Ignorar se não estiver rodando
		if !strings.Contains(string(output), "is not running") {
			return fmt.Errorf("failed to stop task: %s: %w", string(output), err)
		}
	}
	return nil
}

// IsInstalled checks if a service is installed.
func (m *WindowsManager) IsInstalled(name string) (bool, error) {
	// Try both with and without spaces, as Task Scheduler may normalize names
	taskNameWithSpace := fmt.Sprintf("Nazim_%s", name)
	// Remove spaces for alternative check (Task Scheduler sometimes removes them)
	taskNameNoSpace := fmt.Sprintf("Nazim_%s", strings.ReplaceAll(name, " ", ""))
	
	// First try with the original name (with spaces)
	cmd := exec.Command("schtasks", "/query", "/tn", taskNameWithSpace)
	output, err := cmd.CombinedOutput()
	if err == nil {
		// Task found with original name
		return true, nil
	}
	
	// If not found, try without spaces (Task Scheduler may have normalized it)
	outputStr := strings.ToLower(string(output))
	if strings.Contains(outputStr, "does not exist") || 
	   strings.Contains(outputStr, "cannot find") ||
	   strings.Contains(outputStr, "not found") {
		// Try without spaces
		cmd = exec.Command("schtasks", "/query", "/tn", taskNameNoSpace)
		_, err2 := cmd.CombinedOutput()
		if err2 == nil {
			return true, nil
		}
		// Neither version found
		return false, nil
	}
	
	// Other errors (like permission issues)
	return false, nil
}
