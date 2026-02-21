package daemon

// ServiceManager handles system service installation and management.
type ServiceManager struct{}

// NewServiceManager creates a new service manager.
func NewServiceManager() *ServiceManager {
	return &ServiceManager{}
}

// Install installs k2 as a system service and starts it.
// On macOS: launchd plist (kaitu)
// On Linux: systemd unit (k2.service)
func (s *ServiceManager) Install() error {
	return installService()
}

// Uninstall stops and removes the system service.
func (s *ServiceManager) Uninstall() error {
	return uninstallService()
}

// IsInstalled returns true if the system service is installed.
func (s *ServiceManager) IsInstalled() bool {
	return isServiceInstalled()
}
