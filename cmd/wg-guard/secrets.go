package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
)

// runSecrets rotates the node master key (security.md §Master-key rotation):
// a crash-safe dual-key window re-encrypts every stored node field
// old→new, then removes the previous
// key. The service must be stopped — the running node holds the old ring.
//
//	wg-guard secrets rotate [-yes]
func runSecrets(args []string) error {
	if len(args) == 0 || args[0] != "rotate" {
		return fmt.Errorf("usage: wg-guard secrets rotate [-yes] [-config PATH]")
	}
	flags := flag.NewFlagSet("secrets rotate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "/etc/wg-guard/wg-guard.toml", "node configuration")
	yes := flags.Bool("yes", false, "confirm without a prompt")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("secrets rotate: %w", err)
	}
	if len(flags.Args()) != 0 {
		return fmt.Errorf("secrets rotate: unexpected argument")
	}
	if strings.TrimSpace(*configPath) == "" || strings.HasPrefix(*configPath, "-") {
		return fmt.Errorf("secrets rotate: --config requires a path")
	}

	env, err := loadCLIEnvOwnership(*configPath, true)
	if err != nil {
		return err
	}
	defer env.Close()

	if backup.ServiceRunning(env.Cfg.HTTPListen) {
		return fmt.Errorf("the service is running on %s — stop it first (rotation replaces the key file the node holds)", env.Cfg.HTTPListen)
	}
	if !*yes {
		fmt.Println("Rotation generates a new master key and re-encrypts every stored secret")
		fmt.Println("(interface/device keys, customer links, webhook secrets and settings).")
		fmt.Println("It is crash-safe:")
		fmt.Println("an interruption leaves both key versions able to decrypt and the")
		fmt.Println("next run resumes. Archives are unaffected.")
		fmt.Print("Proceed? Type YES to confirm: ")
		var answer string
		if _, err := fmt.Scanln(&answer); err != nil || answer != "YES" {
			fmt.Println("rotation cancelled")
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if _, err := secrets.Rotate(env.Cfg.MasterKeyFile, secrets.NodeCarrier(ctx, env.DB.DB)); err != nil {
		return err
	}
	fmt.Println("master key rotated: all stored secrets were re-encrypted; previous key removed")
	fmt.Println("take a fresh backup so archives match the new key layout")
	return nil
}
