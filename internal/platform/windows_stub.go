//go:build !windows
// +build !windows

// Package platform provides Windows-specific service management stubs for non-Windows builds.
package platform

import "github.com/calilkhalil/nazim/internal/service"

// WindowsManager manages services on Windows using Task Scheduler.
// This is a stub for non-Windows builds.
type WindowsManager struct{}

// NewWindowsManager creates a new manager for Windows.
// This is a stub for non-Windows builds and should never be called.
func NewWindowsManager() *WindowsManager {
	panic("NewWindowsManager should not be called on non-Windows platforms")
}

// Install installs a service on Windows using schtasks.
func (m *WindowsManager) Install(svc *service.Service) error {
	panic("WindowsManager.Install should not be called on non-Windows platforms")
}

// Uninstall removes a service from Windows.
func (m *WindowsManager) Uninstall(name string) error {
	panic("WindowsManager.Uninstall should not be called on non-Windows platforms")
}

// Enable enables a service on Windows.
func (m *WindowsManager) Enable(name string) error {
	panic("WindowsManager.Enable should not be called on non-Windows platforms")
}

// Disable disables a service on Windows.
func (m *WindowsManager) Disable(name string) error {
	panic("WindowsManager.Disable should not be called on non-Windows platforms")
}

// Run executes a service immediately on Windows.
func (m *WindowsManager) Run(name string) error {
	panic("WindowsManager.Run should not be called on non-Windows platforms")
}

// IsInstalled checks if a service is installed.
func (m *WindowsManager) IsInstalled(name string) (bool, error) {
	panic("WindowsManager.IsInstalled should not be called on non-Windows platforms")
}

// GetTaskState returns the state of a scheduled task.
func (m *WindowsManager) GetTaskState(name string) (string, error) {
	panic("WindowsManager.GetTaskState should not be called on non-Windows platforms")
}
