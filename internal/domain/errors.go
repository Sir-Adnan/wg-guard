package domain

import (
	"errors"
	"fmt"
)

// Machine error codes — REST API error-envelope contract (V1 stability).
// Add new codes; never rename or remove existing ones.
const (
	CodeInvalidRequest      = "INVALID_REQUEST"
	CodeUnauthorized        = "UNAUTHORIZED"
	CodeForbidden           = "FORBIDDEN"
	CodeRateLimited         = "RATE_LIMITED"
	CodeNodeUnavailable     = "NODE_UNAVAILABLE"
	CodeInternal            = "INTERNAL_ERROR"
	CodeNotFound            = "NOT_FOUND"
	CodeUserNotFound        = "USER_NOT_FOUND"
	CodeUsernameExists      = "USERNAME_EXISTS"
	CodeDeviceNotFound      = "DEVICE_NOT_FOUND"
	CodeDeviceLimitReached  = "DEVICE_LIMIT_REACHED"
	CodeDevicePoolExhausted = "DEVICE_POOL_EXHAUSTED"
	CodeDeviceKeyExists     = "DEVICE_KEY_EXISTS"
	CodePlanNotFound        = "PLAN_NOT_FOUND"
	CodePlanInUse           = "PLAN_IN_USE"
	CodeInterfaceNotFound   = "INTERFACE_NOT_FOUND"
	CodeInterfaceNameTaken  = "INTERFACE_NAME_TAKEN"
	CodePortInUse           = "PORT_IN_USE"
	CodeSubnetInvalid       = "SUBNET_INVALID"
	CodeSubnetOverlap       = "SUBNET_OVERLAP"
	CodeParamConstraint     = "PARAM_CONSTRAINT"
	CodeCredentialInvalid   = "INVALID_CREDENTIALS"
	CodeSessionExpired      = "SESSION_EXPIRED"
	CodeTokenInvalid        = "TOKEN_INVALID"
	CodeTokenExists         = "TOKEN_EXISTS"
	CodeAdminNotFound       = "ADMIN_NOT_FOUND"
	CodeAdminExists         = "ADMIN_EXISTS"
	CodeOwnerProtected      = "OWNER_PROTECTED"
	CodeSettingUnknown      = "SETTING_UNKNOWN"
	CodeSettingInvalid      = "SETTING_INVALID"
	CodeConfigInvalid       = "CONFIG_INVALID"
)

// Error carries a stable machine code so handlers can map any service error
// to the API envelope without type switches over storage internals. Identity
// is preserved through wrapping (errors.As).
type Error struct {
	Code    string
	Message string
	err     error
}

func E(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap attaches the underlying cause while preserving code and message.
func Wrap(err error, code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), err: err}
}

func (e *Error) Error() string {
	if e.err != nil {
		return fmt.Sprintf("%s (%s): %v", e.Message, e.Code, e.err)
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

func (e *Error) Unwrap() error { return e.err }

// CodeOf returns the machine code of err (or CodeInternal for foreign errors).
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeInternal
}
