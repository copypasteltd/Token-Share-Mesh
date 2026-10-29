// p11-admin is a local native maintenance tool. It never charges money or
// qualifies a device. Credentials are read only from environment variables.
package main

import (
	"context"
	"fmt"
	"os"
	"p11.local/compute/internal/persistence"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Operation failed; inspect the local configuration and database state.")
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) != 2 || (os.Args[1] != "migrate" && os.Args[1] != "check-schema") {
		fmt.Fprintln(os.Stderr, "Usage: p11-admin migrate|check-schema")
		return fmt.Errorf("invalid command")
	}
	dsn := os.Getenv("P11_DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("P11_DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	db, err := persistence.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	if os.Args[1] == "migrate" {
		if err = db.Migrate(ctx); err != nil {
			return err
		}
		fmt.Println("Native domain migrations applied and checksums verified.")
		return nil
	}
	var tables, versions int
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='p11' AND table_type='BASE TABLE'`).Scan(&tables); err != nil {
		return err
	}
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11_meta.schema_migrations`).Scan(&versions); err != nil {
		return err
	}
	if tables != 64 || versions != 4 {
		return fmt.Errorf("incomplete schema")
	}
	fmt.Printf("Domain tables: %d; applied migrations: %d.\n", tables, versions)
	return nil
}
