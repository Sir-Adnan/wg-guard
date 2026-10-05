package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/domainqueue"
	"github.com/Sir-Adnan/wg-guard/internal/domaintls"
	"github.com/Sir-Adnan/wg-guard/internal/install"
)

func runDomains(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("domains: use status, configure, remove, renew, recover or request-run")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	h := install.NewRealHost()
	switch args[0] {
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("domains: unexpected arguments")
		}
		i, e := install.DomainStatus(ctx, h)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(i)
	case "bridge-install":
		if len(args) != 1 {
			return lifecycleArgsError()
		}
		return install.RepairDomainBroker(ctx, h)
	case "request-run":
		if len(args) != 1 {
			return lifecycleArgsError()
		}
		return runDomainRequest(ctx, h, domainqueue.New(install.DataDir))
	case "recover":
		if len(args) != 1 {
			return lifecycleArgsError()
		}
		if err := install.RecoverDomains(ctx, h); err != nil {
			return err
		}
		return install.PublishDomainInventory(ctx, h)
	case "challenge-prepare":
		if len(args) != 1 {
			return lifecycleArgsError()
		}
		return install.PrepareDomainRenewalChallenges(ctx, h)
	case "renew":
		if len(args) != 2 {
			return fmt.Errorf("domains renew requires panel or subscription")
		}
		if err := install.RenewDomainCertificate(ctx, h, domaintls.Role(args[1])); err != nil {
			return err
		}
		return install.PublishDomainInventory(ctx, h)
	case "configure", "remove":
		fs := flag.NewFlagSet("domains "+args[0], flag.ContinueOnError)
		role := fs.String("role", "subscription", "panel | subscription")
		origin := fs.String("origin", "", "HTTPS origin")
		method := fs.String("method", "automatic", "automatic | manual | external")
		challenge := fs.String("challenge", "http", "http | cloudflare")
		email := fs.String("email", "", "ACME account email")
		cert := fs.String("cert-file", "", "controlled certificate source file")
		key := fs.String("key-file", "", "controlled private key source file")
		if e := fs.Parse(args[1:]); e != nil {
			return e
		}
		if fs.NArg() != 0 {
			return fmt.Errorf("domains: unexpected arguments")
		}
		i, e := install.DomainStatus(ctx, h)
		if e != nil {
			return e
		}
		o := install.DomainOptions{Role: domaintls.Role(*role), Origin: *origin, Method: domaintls.Method(*method), ExpectedRevision: i.Revision, Remove: args[0] == "remove", Email: *email, Challenge: *challenge, Stdout: os.Stdout}
		if !o.Remove && o.Method == domaintls.Manual {
			st, e := install.LoadState(h)
			if e != nil || st == nil {
				return fmt.Errorf("domains: installed state required")
			}
			r := domainqueue.Request{Input: domainqueue.Input{Operation: "configure", Role: o.Role, Method: o.Method, CertSource: *cert, KeySource: *key}}
			material, e := install.ReadDomainInput(h, st, r)
			if e != nil {
				return e
			}
			o.CertPEM, o.KeyPEM = material.CertPEM, material.KeyPEM
			defer clear(o.CertPEM)
			defer clear(o.KeyPEM)
		}
		result, e := install.ConfigureDomain(ctx, h, o)
		if e != nil {
			return e
		}
		if err := install.PublishDomainInventory(ctx, h); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	default:
		return fmt.Errorf("domains: unsupported operation")
	}
}

func runDomainRequest(ctx context.Context, h install.Host, q *domainqueue.Queue) error {
	if !h.IsRoot() {
		return fmt.Errorf("domains: root required")
	}
	unlock, e := install.LockMaintenanceRunner()
	if e != nil {
		return e
	}
	defer unlock()
	if e := q.RecoverInterrupted(); e != nil {
		return e
	}
	if q.HasRequest() {
		r, e := q.Claim()
		if e != nil {
			return e
		}
		e = executeDomainRequest(ctx, h, q, r)
		e = errors.Join(e, install.PublishDomainInventory(ctx, h))
		finish := q.Finish(r, e)
		if r.StageID != "" {
			_ = q.RemoveImport(r.StageID)
		}
		if e != nil {
			return errors.Join(e, finish)
		}
		if finish != nil {
			return finish
		}
	} else if e := install.RenewDueDomains(ctx, h); e != nil {
		return e
	}
	i, e := install.DomainStatus(ctx, h)
	if e != nil {
		return e
	}
	return q.WriteInventory(i)
}

func executeDomainRequest(ctx context.Context, h install.Host, q *domainqueue.Queue, r domainqueue.Request) error {
	ctx, cancel := context.WithTimeout(ctx, 18*time.Minute)
	defer cancel()
	release, e := install.AuthorizeDomainOwner(ctx, h, r.ActorID)
	if e != nil {
		_ = q.Record(r, "failed", "validating", "authorization_revoked")
		return fmt.Errorf("domains: authorization revoked")
	}
	defer release()
	if e := q.Record(r, "running", "validating", ""); e != nil {
		return e
	}
	switch r.Operation {
	case "inspect":
		return nil
	case "recover":
		return install.RecoverDomains(ctx, h)
	case "renew":
		inventory, err := install.DomainStatus(ctx, h)
		if err != nil || inventory.Revision != r.ExpectedRevision {
			return domainqueue.ErrInvalid
		}
		return install.RenewDomainCertificate(ctx, h, r.Role)
	case "configure", "remove":
		st, e := install.LoadState(h)
		if e != nil || st == nil {
			return fmt.Errorf("domains: installed state required")
		}
		o, e := install.ReadDomainInput(h, st, r)
		if e != nil {
			return e
		}
		defer clear(o.CertPEM)
		defer clear(o.KeyPEM)
		o.Stdout = io.Discard
		o.Progress = func(stage string) { _ = q.Record(r, "running", stage, "") }
		_, e = install.ConfigureDomain(ctx, h, o)
		return e
	}
	return domainqueue.ErrInvalid
}
