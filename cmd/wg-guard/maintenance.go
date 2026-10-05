package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/amneziawg"
	"github.com/Sir-Adnan/wg-guard/internal/updatequeue"
)

func runUserspaceCheck(args []string) error {
	if len(args) != 0 {
		return updatequeue.ErrInvalid
	}
	file, err := exec.LookPath("amneziawg-go")
	if errors.Is(err, exec.ErrNotFound) {
		fmt.Println("absent")
		return nil
	}
	if err != nil {
		return err
	}
	if err := amneziawg.VerifyUserspaceBinary(file); err != nil {
		return err
	}
	fmt.Println("verified")
	return nil
}

type maintenanceOutput struct {
	io.Writer
	q   *updatequeue.Queue
	req updatequeue.Request
	err error
}

func (o *maintenanceOutput) MaintenanceStep(component, stage, state string) {
	if err := o.q.Record(o.req, component, stage, updatequeue.State(state)); err != nil {
		o.err = errors.Join(o.err, err)
	}
}
func (o *maintenanceOutput) task(component, stage string, run func() error) error {
	o.MaintenanceStep(component, stage, "running")
	if o.err != nil {
		return o.err
	}
	err := run()
	state := "succeeded"
	if err != nil {
		state = "failed"
	}
	o.MaintenanceStep(component, stage, state)
	if err != nil {
		code := updatequeue.Failure("operation_failed")
		switch stage {
		case "acquire":
			code = "acquisition_failed"
		case "preflight":
			code = "preflight_blocked"
		case "backup":
			code = "backup_failed"
		case "health":
			code = "health_failed"
		case "core":
			code = "core_failed"
		case "recovery":
			code = "recovery_failed"
		case "deploy", "image":
			code = "deployment_failed"
		case "verify":
			code = "identity_changed"
		}
		var terminal *install.TerminalError
		if errors.As(err, &terminal) && terminal.Key == "install.error.core.16" {
			code = "reboot_required"
		}
		err = &updatequeue.OperationError{Code: code, Cause: err}
	}
	return errors.Join(err, o.err)
}

func executeMaintenance(ctx context.Context, h install.Host, q *updatequeue.Queue, req updatequeue.Request, out io.Writer) (resultErr error) {
	o := &maintenanceOutput{Writer: out, q: q, req: req}
	if req.ActorID != "" {
		release, err := install.AuthorizeMaintenance(ctx, h, req.ActorID)
		if err != nil {
			return &updatequeue.OperationError{Code: "authorization_revoked", Cause: err}
		}
		defer release()
	}
	defer func() {
		i, err := install.MaintenanceInventory(ctx, h)
		if err == nil {
			old, _ := q.Inventory()
			i.Preflight = old.Preflight
			if resultErr != nil && (req.Operation == updatequeue.OperationPreflight || req.Operation == updatequeue.OperationDownload) {
				i.Preflight = nil
			}
			err = q.PublishInventory(i)
			outcome := updatequeue.Outcome{PanelVersion: i.PanelVersion, ManagerVersion: i.ManagerVersion, Bundle: i.Bundle, Recovery: i.Recovery, RebootRequired: i.RebootRequired}
			if i.Backup != nil && (old.Backup == nil || old.Backup.OperationID != i.Backup.OperationID) {
				outcome.Backup = i.Backup
			}
			err = errors.Join(err, q.RecordOutcome(req, outcome))
		}
		resultErr = errors.Join(resultErr, err, o.err)
	}()
	input := req.Input
	if input.Operation == updatequeue.OperationInspect {
		return o.task("host", "inspect", func() error {
			i, e := install.MaintenanceInventory(ctx, h)
			if e != nil {
				return e
			}
			return q.PublishInventory(i)
		})
	}
	if input.Operation == updatequeue.OperationRecover || input.Operation == updatequeue.OperationRollback {
		return o.task("panel", "recovery", func() error {
			return install.Update(ctx, h, install.UpdateOptions{Recover: input.Operation == updatequeue.OperationRecover, Rollback: input.Operation == updatequeue.OperationRollback, Stdout: o})
		})
	}
	preview := input.Operation == updatequeue.OperationPreflight
	download := input.Operation == updatequeue.OperationDownload
	if preview || download {
		input.Operation, input.Target = input.Target, ""
	}
	if err := install.CheckLifecycleReady(h); err != nil {
		return err
	}
	var build distribution.Build
	var parent string
	cleanup := func() {}
	if input.Operation != updatequeue.OperationCore {
		err := o.task("panel", "acquire", func() error {
			var err error
			build, parent, cleanup, err = prepareMaintenanceArtifact(ctx, h, distribution.Selection{Channel: input.Channel, Ref: input.Ref})
			return err
		})
		if err != nil {
			return err
		}
		defer cleanup()
		if input.ExpectedCommit != "" && build.Commit != input.ExpectedCommit || input.ExpectedSHA256 != "" && build.SHA256 != input.ExpectedSHA256 {
			return o.task("panel", "verify", func() error { return fmt.Errorf("maintenance: selected release identity changed; review again") })
		}
	}
	var candidate *distribution.Build
	if input.Operation != updatequeue.OperationCore {
		candidate = &build
	}
	err := o.task("host", "preflight", func() error {
		i, e := install.MaintenancePreflight(ctx, h, input, candidate)
		if e != nil {
			return e
		}
		if e = q.PublishInventory(i); e != nil {
			return e
		}
		if !i.Preflight.Ready {
			return fmt.Errorf("maintenance: preflight blocked")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if preview || download {
		// Prepared files are separate from both the service and manager cache.
		if candidate == nil {
			return nil
		}
		st, e := install.LoadState(h)
		if e != nil || st == nil {
			return fmt.Errorf("maintenance: installed state required")
		}
		selector := st.Core.Requested.ID
		if input.Operation == updatequeue.OperationAll {
			selector = input.Core
		}
		bundle, e := install.SelectCore(selector)
		if e != nil {
			return e
		}
		if e = o.task("panel", "image", func() error { _, err := install.PrepareRuntimeImage(ctx, h, &build, bundle, parent); return err }); e != nil {
			return e
		}
		return o.task("panel", "verify", func() error {
			return install.CacheMaintenanceBuild(ctx, h, build)
		})
	}
	if input.Operation == updatequeue.OperationCore {
		return o.task("core", "core", func() error {
			_, e := install.SwitchCore(ctx, h, install.CoreSwitchOptions{Selector: input.Core, ConfirmImpact: true, Stdout: o})
			return e
		})
	}
	if err := o.task("manager", "manager", func() error {
		return install.UpdateManager(ctx, h, install.ManagerUpdateOptions{Build: build, Stdout: o})
	}); err != nil {
		return err
	}
	st, err := install.LoadState(h)
	if err != nil || st == nil {
		return errors.Join(err, fmt.Errorf("maintenance: installed state required"))
	}
	bundleID := st.Core.Requested.ID
	if input.Operation == updatequeue.OperationAll {
		bundleID = input.Core
	}
	bundle, err := install.SelectCore(bundleID)
	if err != nil {
		return err
	}
	image := ""

	if err := o.task("panel", "image", func() error {
		var e error
		image, e = install.PrepareRuntimeImage(ctx, h, &build, bundle, parent)
		return e
	}); err != nil {
		return err
	}

	if err := o.task("panel", "deploy", func() error {
		return install.Update(ctx, h, install.UpdateOptions{Build: build, BinaryPath: build.BinaryPath, Image: image, LocalImage: true, Stdout: o})
	}); err != nil {
		return err
	}
	if input.Operation == updatequeue.OperationAll {
		return o.task("core", "core", func() error {
			_, e := install.SwitchCore(ctx, h, install.CoreSwitchOptions{Selector: input.Core, ConfirmImpact: true, Stdout: o})
			return e
		})
	}
	return nil
}

func prepareMaintenanceArtifact(ctx context.Context, h install.Host, selection distribution.Selection) (distribution.Build, string, func(), error) {
	if cached, err := install.LoadPreparedMaintenanceBuild(ctx, h); err == nil && cached.Channel == selection.Channel && cached.Ref == selection.Ref {
		if err := distribution.NewClient(nil, distribution.Options{}).ValidateCachedRelease(ctx, cached); err != nil {
			return distribution.Build{}, "", func() {}, err
		}
		parent, err := newLifecycleStage()
		if err != nil {
			return distribution.Build{}, "", func() {}, err
		}
		return cached, parent, func() { _ = os.Remove(parent) }, nil
	}
	return prepareBuild(ctx, selection, "", nil)
}

func runMaintenanceBroker(ctx context.Context, h install.Host, q *updatequeue.Queue) error {
	if !h.IsRoot() {
		return fmt.Errorf("maintenance: root required")
	}
	unlock, err := install.LockMaintenanceRunner()
	if err != nil {
		return err
	}
	defer unlock()
	if err := q.ReconcileInterrupted(); err != nil {
		return err
	}
	if err := q.PromoteDue(); err != nil && !errors.Is(err, updatequeue.ErrBusy) {
		return err
	}
	err = runUpdateRequestWith(ctx, q, updateResponseGrace, func(_ string, _ []string) error {
		s, e := q.Status()
		if e != nil {
			return e
		}
		req := updatequeue.Request{Schema: s.Schema, ID: s.ID, CreatedAt: s.CreatedAt, Actor: s.Actor, ActorID: s.ActorID, ExecuteAt: s.ExecuteAt, Input: s.Input}
		return executeMaintenance(ctx, h, q, req, os.Stdout)
	})
	if errors.Is(err, updatequeue.ErrNoRequest) {
		i, e := q.Inventory()
		if e == nil && !i.ObservedAt.IsZero() && time.Since(i.ObservedAt) < 10*time.Minute {
			return nil
		}
		i, e = install.MaintenanceInventory(ctx, h)
		if e != nil {
			return e
		}
		return q.PublishInventory(i)
	}
	return err
}
