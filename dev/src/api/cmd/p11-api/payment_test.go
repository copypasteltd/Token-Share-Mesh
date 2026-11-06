package main

// SOURCE_ONLY. Configuration boundaries only; no host/database/RPC is started.
import "testing"

func TestOptionalPaymentHostRequiresCompleteConfiguration(t *testing.T) {
	t.Setenv("P11_MACHINE_PAY_ROOT", "")
	t.Setenv("P11_MACHINE_PAY_CONFIG", "")
	h, e := configuredPayments(nil)
	if e != nil || h != nil {
		t.Fatal("absent payment configuration did not remain disabled")
	}
	t.Setenv("P11_MACHINE_PAY_ROOT", "explicit_root")
	if h, e = configuredPayments(nil); e == nil || h != nil {
		t.Fatal("partial payment configuration silently enabled or disabled")
	}
}
func TestPaymentHostCannotReuseOAuthConfigurationImplicitly(t *testing.T) {
	t.Setenv("P11_MACHINE_PAY_ROOT", "")
	t.Setenv("P11_MACHINE_PAY_CONFIG", "")
	t.Setenv("P11_ALIPAY_PRIVATE_KEY_FILE", "login_key_not_a_payment_registration")
	t.Setenv("P11_ALIPAY_PUBLIC_KEY_FILE", "login_verifier_not_a_seller_registration")
	h, e := configuredPayments(nil)
	if e != nil || h != nil {
		t.Fatal("OAuth configuration implicitly enabled collection")
	}
}
