package domain

// User lifecycle. Statuses and disable reasons are REST API enum values —
// additive changes only (V1 contract).
type UserStatus string

const (
	UserActive                 UserStatus = "active"
	UserDisabled               UserStatus = "disabled"
	UserSuspended              UserStatus = "suspended"
	UserExpired                UserStatus = "expired"
	UserTrafficExceeded        UserStatus = "traffic_exceeded"
	UserWaitingFirstConnection UserStatus = "waiting_first_connection"
)

func (s UserStatus) Valid() bool {
	switch s {
	case UserActive, UserDisabled, UserSuspended, UserExpired,
		UserTrafficExceeded, UserWaitingFirstConnection:
		return true
	}
	return false
}

// PeerWanted reports whether a user in this status should have live peers in
// the tunnel backend (reconciliation desire).
func (s UserStatus) PeerWanted() bool {
	return s == UserActive || s == UserWaitingFirstConnection
}

type DisableReason string

const (
	DisableManual       DisableReason = "manual"
	DisableExpired      DisableReason = "expired"
	DisableTrafficLimit DisableReason = "traffic_limit"
	DisableAdminAction  DisableReason = "admin_action"
)

func (r DisableReason) Valid() bool {
	switch r {
	case DisableManual, DisableExpired, DisableTrafficLimit, DisableAdminAction:
		return true
	}
	return false
}

type StartPolicy string

const (
	StartImmediate       StartPolicy = "immediate"
	StartFirstConnection StartPolicy = "first_connection"
)

func (p StartPolicy) Valid() bool {
	return p == StartImmediate || p == StartFirstConnection
}

// Tunnel interface lifecycle enums.
type BackendMode string

const (
	BackendKernel    BackendMode = "kernel" // kernel module (primary)
	BackendUserspace BackendMode = "userspace"
)

func (m BackendMode) Valid() bool { return m == BackendKernel || m == BackendUserspace }
