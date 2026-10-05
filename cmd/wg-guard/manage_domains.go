package main

import (
	"context"
	"fmt"

	"github.com/Sir-Adnan/wg-guard/internal/domaintls"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
)

func (m *manager) domainsMenu(ctx context.Context) error {
	for {
		n, err := m.ui.Choose("Independent domains and HTTPS", []string{"Current domain status", "Configure subscription address", "Configure panel address", "Check subscription renewal", "Check panel renewal", "Use panel origin for subscriptions", "Recover interrupted domain operation"}, 0)
		if err != nil {
			return err
		}
		var args []string
		switch n {
		case 1:
			args = []string{"domains", "status"}
		case 2, 3:
			role := "subscription"
			if n == 3 {
				role = "panel"
			}
			args, err = m.domainForm(role)
			if err != nil {
				return err
			}
		case 4:
			args = []string{"domains", "renew", "subscription"}
		case 5:
			args = []string{"domains", "renew", "panel"}
		case 6:
			ok, e := m.ui.Confirm("Remove the separate public address and generate links on the panel origin?")
			if e != nil {
				return e
			}
			if !ok {
				continue
			}
			args = []string{"domains", "remove", "--role", "subscription"}
		case 7:
			args = []string{"domains", "recover"}
		}
		err = m.run(ctx, args, nil)
		m.ui.Result(err)
		if ctx.Err() != nil {
			return terminal.ErrCanceled
		}
	}
}

func (m *manager) domainForm(role string) ([]string, error) {
	origin, err := m.ui.Ask("HTTPS origin (no path)", "")
	if err != nil {
		return nil, err
	}
	o, err := domaintls.ParseOrigin(origin)
	if err != nil {
		return nil, fmt.Errorf("enter a complete HTTPS origin without path or credentials")
	}
	n, err := m.ui.Choose("Certificate ownership", []string{"Automatic HTTPS · HTTP-01", "Automatic HTTPS · existing Cloudflare DNS credentials", "Manual certificate · controlled host files", "Existing external proxy"}, 1)
	if err != nil {
		return nil, err
	}
	method := "automatic"
	args := []string{"domains", "configure", "--role", role, "--origin", o.URL}
	if n == 1 || n == 2 {
		challenge := "http"
		if n == 2 {
			challenge = "cloudflare"
		}
		args = append(args, "--challenge", challenge)
		email, e := m.ui.Ask("ACME email (optional)", "")
		if e != nil {
			return nil, e
		}
		if email != "" {
			args = append(args, "--email", email)
		}
	} else if n == 3 {
		method = "manual"
		base := "/etc/wg-guard/certificate-import/" + role
		cert, e := m.ui.Ask("Certificate chain path", base+"/fullchain.pem")
		if e != nil {
			return nil, e
		}
		key, e := m.ui.Ask("Private key path", base+"/privkey.pem")
		if e != nil {
			return nil, e
		}
		args = append(args, "--cert-file", cert, "--key-file", key)
	} else {
		method = "external"
	}
	args = append(args, "--method", method)
	m.ui.Section("Review domain operation")
	m.ui.Field("Role", role)
	m.ui.Field("Origin", o.URL)
	m.ui.Field("Certificate", method)
	m.ui.Text("VPN endpoints, customer tokens and device keys are preserved. Remote gateway TLS/routing remains operator-owned.")
	if role == "panel" {
		m.ui.Text("Open the new panel address and sign in again after activation; the old hostname becomes public subscriptions when required.")
	}
	ok, err := m.ui.Confirm("Prepare and activate this reviewed address?")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, terminal.ErrBack
	}
	return args, nil
}
