package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
)

// runSecrets rotates the node master key (security.md §Master-key rotation):
// a crash-safe dual-key window re-encrypts every carrier (device keys,
// interface keys, encrypted settings) old→new, then removes the previous
// key. The service must be stopped — the running node holds the old ring.
//
//	wg-guard secrets rotate [-yes]
func runSecrets(args []string) error {
	var (
		yes        bool
		configPath = "/etc/wg-guard/wg-guard.toml"
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "rotate": // subcommand form: wg-guard secrets rotate …
		case "-config", "--config":
			i++
			configPath = args[i]
		case "-yes", "--yes":
			yes = true
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}

	env, err := loadCLIEnvOwnership(configPath, true)
	if err != nil {
		return err
	}
	defer env.Close()

	if backup.ServiceRunning(env.Cfg.HTTPListen) {
		return fmt.Errorf("the service is running on %s — stop it first (rotation replaces the key file the node holds)", env.Cfg.HTTPListen)
	}
	if !yes {
		fmt.Println("Rotation generates a new master key and re-encrypts every stored secret")
		fmt.Println("(device keys, interface keys, encrypted settings). It is crash-safe:")
		fmt.Println("an interruption leaves both key versions able to decrypt and the")
		fmt.Println("next run resumes. Archives are unaffected.")
		fmt.Print("Proceed? Type YES to confirm: ")
		var answer string
		if _, err := fmt.Scanln(&answer); err != nil || answer != "YES" {
			fmt.Println("rotation cancelled")
			return nil
		}
	}

	// Carriers: device keys, interface private keys, encrypted settings.
	carriers := []secrets.Carrier{
		device.NewService(env.DB, env.Ring),
		iface.NewService(env.DB, env.Reg, env.Ring),
		env.Reg,
	}
	quiet := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if _, err := secrets.Rotate(env.Cfg.MasterKeyFile, carriers...); err != nil {
		_ = quiet
		return err
	}
	fmt.Println("master key rotated: all stored secrets were re-encrypted; previous key removed")
	fmt.Println("take a fresh backup so archives match the new key layout")
	return nil
}
