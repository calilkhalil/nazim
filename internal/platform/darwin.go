// Package platform provides macOS-specific service management.
package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"nazim/internal/service"
)

// DarwinManager manages services on macOS using launchd.
type DarwinManager struct{}

// NewDarwinManager creates a new manager for macOS.
func NewDarwinManager() *DarwinManager {
	return &DarwinManager{}
}

// Install installs a service on macOS using launchd.
func (m *DarwinManager) Install(svc *service.Service) error {
	// Criar diretório LaunchAgents se não existir
	launchAgentsDir := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents")
	if err := os.MkdirAll(launchAgentsDir, 0755); err != nil {
		return fmt.Errorf("failed to create LaunchAgents directory: %w", err)
	}

	plistFile := filepath.Join(launchAgentsDir, fmt.Sprintf("com.nazim.%s.plist", svc.Name))

	// Construir conteúdo do plist
	var content strings.Builder
	content.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	content.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	content.WriteString("<plist version=\"1.0\">\n")
	content.WriteString("<dict>\n")
	content.WriteString(fmt.Sprintf("  <key>Label</key>\n  <string>com.nazim.%s</string>\n", svc.Name))
	content.WriteString("  <key>ProgramArguments</key>\n")
	content.WriteString("  <array>\n")
	
	// Dividir comando e argumentos
	parts := strings.Fields(svc.Command)
	for _, part := range parts {
		content.WriteString(fmt.Sprintf("    <string>%s</string>\n", part))
	}
	for _, arg := range svc.Args {
		content.WriteString(fmt.Sprintf("    <string>%s</string>\n", arg))
	}
	content.WriteString("  </array>\n")

	if svc.WorkDir != "" {
		content.WriteString(fmt.Sprintf("  <key>WorkingDirectory</key>\n  <string>%s</string>\n", svc.WorkDir))
	}

	if svc.OnStartup {
		content.WriteString("  <key>RunAtLoad</key>\n  <true/>\n")
	}

	if svc.GetInterval() > 0 {
		content.WriteString("  <key>StartInterval</key>\n")
		content.WriteString(fmt.Sprintf("  <integer>%d</integer>\n", int(svc.GetInterval().Seconds())))
	}

	content.WriteString("  <key>StandardOutPath</key>\n")
	content.WriteString(fmt.Sprintf("  <string>%s</string>\n", filepath.Join(os.Getenv("HOME"), ".nazim", "logs", fmt.Sprintf("%s.out", svc.Name))))
	content.WriteString("  <key>StandardErrorPath</key>\n")
	content.WriteString(fmt.Sprintf("  <string>%s</string>\n", filepath.Join(os.Getenv("HOME"), ".nazim", "logs", fmt.Sprintf("%s.err", svc.Name))))

	content.WriteString("</dict>\n")
	content.WriteString("</plist>\n")

	// Criar diretório de logs se não existir
	logDir := filepath.Join(os.Getenv("HOME"), ".nazim", "logs")
	os.MkdirAll(logDir, 0755)

	if err := os.WriteFile(plistFile, []byte(content.String()), 0644); err != nil {
		return fmt.Errorf("failed to write plist file: %w", err)
	}

	// Carregar o serviço
	cmd := exec.Command("launchctl", "load", plistFile)
	if err := cmd.Run(); err != nil {
		// Tentar unload primeiro se já existir
		exec.Command("launchctl", "unload", plistFile).Run()
		cmd = exec.Command("launchctl", "load", plistFile)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to load service: %w", err)
		}
	}

	return nil
}

// Uninstall removes a service from macOS.
func (m *DarwinManager) Uninstall(name string) error {
	plistFile := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", fmt.Sprintf("com.nazim.%s.plist", name))
	
	// Descarregar primeiro
	exec.Command("launchctl", "unload", plistFile).Run()
	
	// Remover arquivo
	if err := os.Remove(plistFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove plist file: %w", err)
	}

	return nil
}

// Start starts a service on macOS.
func (m *DarwinManager) Start(name string) error {
	label := fmt.Sprintf("com.nazim.%s", name)
	cmd := exec.Command("launchctl", "start", label)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start service: %s: %w", string(output), err)
	}
	return nil
}

// Stop stops a service on macOS.
func (m *DarwinManager) Stop(name string) error {
	label := fmt.Sprintf("com.nazim.%s", name)
	cmd := exec.Command("launchctl", "stop", label)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Ignorar se não estiver rodando
		if !strings.Contains(string(output), "Could not find service") {
			return fmt.Errorf("failed to stop service: %s: %w", string(output), err)
		}
	}
	return nil
}

// IsInstalled checks if a service is installed.
func (m *DarwinManager) IsInstalled(name string) (bool, error) {
	plistFile := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", fmt.Sprintf("com.nazim.%s.plist", name))
	_, err := os.Stat(plistFile)
	return err == nil, nil
}

