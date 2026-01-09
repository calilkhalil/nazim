// Package platform provides Windows-specific service management.
package platform

import (
	"fmt"
	"os/exec"
	"strings"

	"nazim/internal/service"
)

// WindowsManager manages services on Windows using Task Scheduler.
type WindowsManager struct{}

// NewWindowsManager creates a new manager for Windows.
func NewWindowsManager() *WindowsManager {
	return &WindowsManager{}
}

// Install installs a service on Windows using schtasks.
func (m *WindowsManager) Install(svc *service.Service) error {
	// Primeiro, remover se já existir
	m.Uninstall(svc.Name)

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
