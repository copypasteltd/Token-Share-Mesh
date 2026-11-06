// p11-payment-worker runs only original-proof verification and logical expiry.
// It never creates payments, refunds, orders, execution leases or payouts.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"p11.local/compute/internal/paymenthost"
	"p11.local/compute/internal/persistence"
)

func run() error {
	if len(os.Args) != 1 {
		return paymenthost.ErrConfiguration
	}
	dsn, root, name := os.Getenv("P11_DATABASE_URL"), os.Getenv("P11_MACHINE_PAY_ROOT"), os.Getenv("P11_MACHINE_PAY_CONFIG")
	if dsn == "" || root == "" || name == "" {
		return paymenthost.ErrConfiguration
	}
	start, end := context.WithTimeout(context.Background(), 15*time.Second)
	db, e := persistence.Open(start, dsn)
	end()
	if e != nil {
		return fmt.Errorf("native payment database unavailable")
	}
	defer db.Close()
	check, cancelCheck := context.WithTimeout(context.Background(), 5*time.Second)
	e = db.VerifyMigrations(check)
	cancelCheck()
	if e != nil {
		return fmt.Errorf("apply original forward migrations before payment worker startup")
	}
	host, e := paymenthost.Open(db, root, name, os.Getenv("P11_PAYMENT_ENVIRONMENT"))
	if e != nil {
		return e
	}
	// Never drop keys or DB while an actual channel/body/save still runs.
	defer func() { _ = host.Close(context.Background()) }()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return host.RunRecovery(ctx)
}
func main() {
	if run() != nil {
		fmt.Fprintln(os.Stderr, "Original payment worker stopped: explicit configuration and current original authority required")
		os.Exit(1)
	}
}
