//go:build !linux

package install

func (realHost) LockLifecycle() (func(), error) {
	return nil, terminalError("install.error.platform.1")
}
