package plan

import (
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

// ApplyToUser copies a technical template's entitlement fields onto a create
// request. Identity, owner, note and tags remain caller-owned. Creation with a
// template is deterministic even if stale or forged manual fields accompany
// it; editing an existing user does not call this helper.
func ApplyToUser(p *Plan, in user.Input) user.Input {
	in.TemplateID = domain.OptString{Set: true, Value: p.ID}
	in.TrafficLimitBytes = optInt64ForCreate(p.TrafficLimitBytes)
	in.DeviceLimit = optIntForCreate(p.DeviceLimit)
	in.SpeedLimitDownKbps = optIntForCreate(p.SpeedLimitDownKbps)
	in.SpeedLimitUpKbps = optIntForCreate(p.SpeedLimitUpKbps)
	in.InterfaceID = optStringForCreate(p.InterfaceID)
	in.StartPolicy = p.StartPolicy
	in.DurationSeconds = p.DurationSeconds
	in.ExpiresAt = nil
	return in
}

func optInt64ForCreate(v *int64) domain.OptInt64 {
	if v == nil {
		return domain.OptInt64{}
	}
	return domain.OptInt64{Set: true, Value: *v}
}

func optIntForCreate(v *int) domain.OptInt {
	if v == nil {
		return domain.OptInt{}
	}
	return domain.OptInt{Set: true, Value: *v}
}

func optStringForCreate(v *string) domain.OptString {
	if v == nil {
		return domain.OptString{}
	}
	return domain.OptString{Set: true, Value: *v}
}
