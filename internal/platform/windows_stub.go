//go:build !windows
// +build !windows

// Package platform provides Windows-specific service management stubs for non-Windows builds.
package platform

import "nazim/internal/service"

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

// Start starts a service on Windows.
func (m *WindowsManager) Start(name string) error {
	panic("WindowsManager.Start should not be called on non-Windows platforms")
}

// Stop stops a service on Windows.
func (m *WindowsManager) Stop(name string) error {
	panic("WindowsManager.Stop should not be called on non-Windows platforms")
}

// IsInstalled checks if a service is installed.
func (m *WindowsManager) IsInstalled(name string) (bool, error) {
	panic("WindowsManager.IsInstalled should not be called on non-Windows platforms")
}
