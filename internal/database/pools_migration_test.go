package database

import (
	"context"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/migrations"
)

func TestPoolMigrationPreservesExistingAddressAndDeletionState(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.ensureMigrationsTable(ctx); err != nil {
		t.Fatal(err)
	}
	names, _ := fs.Glob(migrations.FS, "*.sql")
	sort.Strings(names)
	for _, name := range names {
		if strings.HasPrefix(name, "0015_") {
			break
		}
		raw, _ := migrations.Read(name)
		if _, err := db.Exec(string(raw)); err != nil {
			t.Fatal(err)
		}
		_, _ = db.Exec(`INSERT INTO migrations(version,applied_at) VALUES(?,'test')`, name)
	}
	_, err := db.Exec(`INSERT INTO tunnel_interfaces(id,name,listen_port,ipv4_subnet,mtu,public_key,private_key_encrypted,created_at,updated_at)
	VALUES('ifc','awg0',39001,'10.8.0.0/24',1420,'fixture',X'01','test','test')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO users(id,username,deleted_at,created_at,updated_at) VALUES('user','retained','2026-01-01T00:00:00Z','test','test')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO devices(id,user_id,interface_id,name,ipv4_address,public_key,private_key_encrypted,created_at,updated_at)
	VALUES('device','user','ifc','phone','10.8.0.128/32','public',X'01','test','test')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var primary, extras, addr, deleted string
	_ = db.QueryRow(`SELECT ipv4_subnet,ipv4_extra_pools FROM tunnel_interfaces`).Scan(&primary, &extras)
	_ = db.QueryRow(`SELECT ipv4_address FROM devices`).Scan(&addr)
	_ = db.QueryRow(`SELECT deleted_at FROM users`).Scan(&deleted)
	if primary != "10.8.0.0/24" || extras != "[]" || addr != "10.8.0.128/32" || deleted == "" {
		t.Fatal("migration reassigned existing addresses or purged accounts")
	}
}
