package install

import "io"

// ProgressReporter is optional presentation output. Implementations accept only
// closed stage IDs; raw stdout/stderr remains in the existing private host log.
type ProgressReporter interface {
	MaintenanceStep(component, stage, state string)
}

func ReportStep(out io.Writer, component, stage, state string) {
	if reporter, ok := out.(ProgressReporter); ok {
		reporter.MaintenanceStep(component, stage, state)
	}
}

func maintenanceTask(out io.Writer, component, stage, label string, run func() error) error {
	ReportStep(out, component, stage, "running")
	err := trackedTask(out, label, run)
	state := "succeeded"
	if err != nil {
		state = "failed"
	}
	ReportStep(out, component, stage, state)
	return err
}
