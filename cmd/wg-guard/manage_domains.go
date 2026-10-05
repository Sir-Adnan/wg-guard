package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/domaintls"
	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
)

func (m *manager) domainsMenu(ctx context.Context) error {
	m.ui.Text("Panel and subscription addresses have separate SSL certificates. Automatic certificates renew when due.")
	m.ui.Text("For a private node, use First HTTPS setup before configuring domain certificates.")
	for {
		n, err := m.ui.Choose("Domains & SSL certificates", []string{"Domain and SSL status", "Subscription domain · obtain / replace SSL", "Panel domain · obtain / replace SSL", "Check / renew subscription SSL", "Check / renew panel SSL", "Use panel address for subscriptions", "Recover interrupted domain / SSL change", "First HTTPS setup / access method"}, 0)
		if err != nil {
			return err
		}
		var args []string
		switch n {
		case 1:
			if m.domainStatus != nil {
				inventory, e := m.domainStatus(ctx)
				if e != nil {
					m.ui.Result(e)
				} else {
					m.showDomainStatus(inventory)
				}
				continue
			}
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
		case 8:
			args = []string{"exposure", "configure"}
		}
		err = m.run(ctx, args, nil)
		m.ui.Result(err)
		if ctx.Err() != nil {
			return terminal.ErrCanceled
		}
	}
}

func (m *manager) showDomainStatus(inventory install.DomainInventory) {
	m.ui.Section("Domain and SSL status")
	m.ui.Field("Panel address", inventory.PanelOrigin)
	m.ui.Field("Subscription address", inventory.SubscriptionOrigin)
	m.ui.Field("Access", string(inventory.Exposure))
	for _, certificate := range inventory.Certificates {
		m.ui.Section(string(certificate.Site.Role) + " SSL certificate")
		m.ui.Field("Status", certificate.State)
		m.ui.Field("Ownership", string(certificate.Site.Method))
		if !certificate.Info.NotAfter.IsZero() {
			m.ui.Field("Expires (UTC)", certificate.Info.NotAfter.UTC().Format("2006-01-02 15:04"))
		}
		if certificate.Info.Automatic {
			m.ui.Field("Renewal", "Scheduled due checks; manual check available")
		} else if certificate.Site.Method == domaintls.Builtin {
			m.ui.Field("Renewal", "Managed automatically by the running panel")
		}
	}
}

func (m *manager) domainForm(role string) ([]string, error) {
	origin, err := m.ui.Ask("Domain or subdomain (HTTPS; no path)", "")
	if err != nil {
		return nil, err
	}
	origin = strings.TrimSpace(origin)
	if !strings.Contains(origin, "://") {
		origin = "https://" + origin
	}
	o, err := domaintls.ParseOrigin(origin)
	if err != nil {
		return nil, fmt.Errorf("enter a complete HTTPS origin without path or credentials")
	}
	n, err := m.ui.Choose("How to obtain the SSL certificate", []string{"Automatic SSL · HTTP-01 (recommended)", "Automatic SSL · existing Cloudflare DNS credentials", "Manual SSL · controlled host certificate files", "Existing external proxy"}, 1)
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
