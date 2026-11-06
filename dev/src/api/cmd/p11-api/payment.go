package main

import (
	"os"
	"p11.local/compute/internal/paymenthost"
	"p11.local/compute/internal/persistence"
)

// A complete absent pair disables collection. Partial configuration is an
// error. OAuth login keys are never reused as personal-seller payment keys.
func configuredPayments(db *persistence.DB) (*paymenthost.Host, error) {
	root, name := os.Getenv("P11_MACHINE_PAY_ROOT"), os.Getenv("P11_MACHINE_PAY_CONFIG")
	if root == "" && name == "" {
		return nil, nil
	}
	if root == "" || name == "" {
		return nil, paymenthost.ErrConfiguration
	}
	return paymenthost.Open(db, root, name, os.Getenv("P11_PAYMENT_ENVIRONMENT"))
}
