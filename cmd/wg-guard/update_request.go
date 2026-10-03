package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/updatequeue"
)

const updateResponseGrace = 2 * time.Second

func updateRequestArgs(input updatequeue.Input) (string, []string, error) {
	if err := updatequeue.ValidateInput(input); err != nil {
		return "", nil, err
	}
	switch input.Operation {
	case updatequeue.OperationInspect, updatequeue.OperationPreflight, updatequeue.OperationDownload, updatequeue.OperationRollback, updatequeue.OperationRecover:
		return string(input.Operation), nil, nil
	case updatequeue.OperationPanel:
		return "panel", []string{"--release", input.Ref}, nil
	case updatequeue.OperationCore:
		if _, err := install.SelectCore(input.Core); err != nil {
			return "", nil, err
		}
		return "core", []string{"--bundle", input.Core, "--yes"}, nil
	case updatequeue.OperationAll:
		if _, err := install.SelectCore(input.Core); err != nil {
			return "", nil, err
		}
		return "all", []string{"--release", input.Ref, "--bundle", input.Core, "--yes"}, nil
	default:
		return "", nil, updatequeue.ErrInvalid
	}
}

func runUpdateRequest(args []string) error {
	if len(args) != 0 {
		return lifecycleArgsError()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 44*time.Minute)
	defer cancel()
	return runMaintenanceBroker(ctx, install.NewRealHost(), updatequeue.New(install.DataDir))
}

func runUpdateRequestWith(ctx context.Context, queue *updatequeue.Queue, grace time.Duration, execute func(string, []string) error) error {
	request, err := queue.Claim(ctx)
	if err != nil {
		return err
	}
	if wait := time.Until(request.CreatedAt.Add(grace)); wait > 0 {
		if wait > grace {
			wait = grace
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			err = ctx.Err()
		case <-timer.C:
		}
	}
	var operationErr error
	if err != nil {
		operationErr = err
	} else {
		kind, runArgs, argErr := updateRequestArgs(request.Input)
		if argErr != nil {
			operationErr = argErr
		} else {
			operationErr = execute(kind, runArgs)
		}
	}
	finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return errors.Join(operationErr, queue.Finish(finishCtx, request, operationErr))
}

func runUpdateBrokerInstall(args []string) error {
	if len(args) != 0 {
		return lifecycleArgsError()
	}
	host := install.NewRealHost()
	if !host.IsRoot() {
		return fmt.Errorf("update broker: root is required")
	}
	unlock, err := host.LockLifecycle()
	if err != nil {
		return err
	}
	defer unlock()
	state, err := install.LoadState(host)
	if err != nil {
		return err
	}
	if state == nil {
		return fmt.Errorf("update broker: WG-Guard is not installed")
	}
	contract := install.CurrentContract()
	if state.Current != nil {
		contract = state.Current.Contract
	}
	if err := install.EnsureUpdateBrokerForContract(context.Background(), host, contract); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(os.Stdout, "WG-Guard host update bridge is ready.")
	return nil
}
