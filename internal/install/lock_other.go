//go:build !linux

package install

func LockMaintenanceRunner() (func(), error) { return nil, terminalError("install.error.platform.1") }

func (realHost) LockLifecycle() (func(), error) {
	return nil, terminalError("install.error.platform.1")
}
