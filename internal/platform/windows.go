// Package platform provides Windows-specific service management.
package platform

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"nazim/internal/service"
)

// WindowsManager manages services on Windows using Task Scheduler.
type WindowsManager struct{}

// NewWindowsManager creates a new manager for Windows.
func NewWindowsManager() *WindowsManager {
	return &WindowsManager{}
}

// isAdmin checks if the current process is running with administrator privileges.
// It does this by attempting to query a system task, which requires admin privileges.
func isAdmin() bool {
	if runtime.GOOS != "windows" {
		return false
	}

	// Try to query a system-level task (requires admin)
	// This is a lightweight check that doesn't require external dependencies
	cmd := exec.Command("schtasks", "/query", "/tn", "\\Microsoft\\Windows\\Defrag\\ScheduledDefrag")
	err := cmd.Run()
	// If we can query system tasks, we have admin privileges
	return err == nil
}

// requestElevation attempts to re-run the current process with administrator privileges.
func requestElevation() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("elevation is only supported on Windows")
	}

	// Get the current executable path
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Get command line arguments
	args := os.Args[1:]
	
	// Build PowerShell array syntax for arguments
	// This is more reliable than passing as a single string
	var argArrayParts []string
	for _, arg := range args {
		// Escape single quotes for PowerShell and wrap in single quotes
		escaped := strings.ReplaceAll(arg, `'`, `''`)
		argArrayParts = append(argArrayParts, fmt.Sprintf(`'%s'`, escaped))
	}
	argArrayStr := strings.Join(argArrayParts, ",")

	// Use PowerShell to request elevation
	// The -Verb RunAs will trigger UAC prompt
	// Pass arguments as a PowerShell array for better reliability
	psCmd := fmt.Sprintf(
		`$proc = Start-Process -FilePath '%s' -ArgumentList @(%s) -Verb RunAs -PassThru -Wait; if ($proc) { exit $proc.ExitCode } else { exit 1 }`,
		strings.ReplaceAll(exe, `'`, `''`), argArrayStr,
	)

	psArgs := []string{
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-Command", psCmd,
	}

	cmd := exec.Command("powershell", psArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	// Run and wait for completion
	// If user cancels UAC, this will return an error
	if err := cmd.Run(); err != nil {
		// Check if it's a cancellation (user denied UAC)
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode := exitError.ExitCode()
			if exitCode == 1 || exitCode == 1223 {
				// 1223 is ERROR_CANCELLED in Windows
				return fmt.Errorf("elevation was cancelled or denied - please approve the UAC prompt to continue")
			}
		}
		return fmt.Errorf("failed to elevate privileges: %w", err)
	}

	// If we get here, the elevated process completed successfully
	// Exit this non-elevated process
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
	taskName := fmt.Sprintf("Nazim_%s", name)
	cmd := exec.Command("schtasks", "/query", "/tn", taskName)
	err := cmd.Run()
	return err == nil, nil
}
