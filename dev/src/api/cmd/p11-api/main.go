// p11-api runs native loopback control APIs. It never starts engines or payments.
package main

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	controlhttp "p11.local/compute/api/internal/http"
	"p11.local/compute/internal/dispatch/device"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/market"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/internal/routecontract"
	"p11.local/compute/internal/seller"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/clock"
	"p11.local/compute/shared/ids"
	"strconv"
	"syscall"
	"time"
)

func readSecret(variable string) ([]byte, error) {
	path := os.Getenv(variable)
	if path == "" {
		return nil, fmt.Errorf("required configuration %s", variable)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) < 32 || len(data) > 16384 {
		return nil, fmt.Errorf("invalid secret file for %s", variable)
	}
	return data, nil
}
func privateKey(data []byte) (*rsa.PrivateKey, error) {
	block, rest := pem.Decode(data)
	if block == nil || len(rest) != 0 {
		return nil, fmt.Errorf("one RSA private key PEM required")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("PKCS1 or PKCS8 RSA private key required")
	}
	value, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("RSA private key required")
	}
	return value, nil
}
func publicKey(data []byte) (*rsa.PublicKey, error) {
	block, rest := pem.Decode(data)
	if block == nil || len(rest) != 0 {
		return nil, fmt.Errorf("one Alipay public key PEM required")
	}
	if key, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return key, nil
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("RSA public key required; certificate mode is not configured")
	}
	value, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("RSA public key required")
	}
	return value, nil
}
func run() error {
	if len(os.Args) != 1 {
		return fmt.Errorf("configure p11-api through explicit environment variables")
	}
	var fees *seller.FeePolicy
	feeVersion, bpsText, costOwner := os.Getenv("P11_FEE_POLICY_VERSION"), os.Getenv("P11_COMMISSION_BPS"), os.Getenv("P11_CHANNEL_COST_OWNER")
	if feeVersion != "" || bpsText != "" || costOwner != "" {
		bps, err := strconv.Atoi(bpsText)
		if err != nil || bps < 0 || bps > 10000 || !ids.Valid(feeVersion) || (costOwner != "SELLER" && costOwner != "PLATFORM") {
			return fmt.Errorf("explicit complete seller fee policy required")
		}
		fees = &seller.FeePolicy{BPS: bps, Version: feeVersion, ChannelCostOwner: costOwner}
	}
	dsn := os.Getenv("P11_DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("P11_DATABASE_URL required")
	}
	signing, err := readSecret("P11_ALIPAY_PRIVATE_KEY_FILE")
	if err != nil {
		return err
	}
	verification, err := readSecret("P11_ALIPAY_PUBLIC_KEY_FILE")
	if err != nil {
		return err
	}
	private, err := privateKey(signing)
	if err != nil {
		return err
	}
	public, err := publicKey(verification)
	if err != nil {
		return err
	}
	provider, err := identity.NewAlipay(identity.AlipayConfig{AppID: os.Getenv("P11_ALIPAY_APP_ID"),
		SubjectField: os.Getenv("P11_ALIPAY_SUBJECT_FIELD"), WebRedirect: os.Getenv("P11_ALIPAY_OAUTH_RETURN_URL"),
		PrivateKey: private, PublicKey: public})
	if err != nil {
		return err
	}
	authPepper, err := readSecret("P11_AUTH_PEPPER_FILE")
	if err != nil {
		return err
	}
	identityPepper, err := readSecret("P11_IDENTITY_PEPPER_FILE")
	if err != nil {
		return err
	}
	keyPepper, err := readSecret("P11_API_KEY_PEPPER_FILE")
	if err != nil {
		return err
	}
	cursorKey, err := readSecret("P11_CURSOR_KEY_FILE")
	if err != nil {
		return err
	}
	var pairPepper []byte
	if os.Getenv("P11_DEVICE_PAIRING_PEPPER_FILE") != "" {
		pairPepper, err = readSecret("P11_DEVICE_PAIRING_PEPPER_FILE")
		if err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	db, err := persistence.Open(ctx, dsn)
	cancel()
	if err != nil {
		return err
	}
	defer db.Close()
	checkCtx, checkCancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = db.VerifyMigrations(checkCtx)
	checkCancel()
	if err != nil {
		return fmt.Errorf("apply and verify migrations with p11-admin before starting control APIs")
	}
	address := os.Getenv("P11_API_ADDR")
	if address == "" {
		address = "127.0.0.1:8787"
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return fmt.Errorf("numeric loopback API address required")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	claims, err := configuredClaims(db)
	if err != nil {
		return err
	}
	if claims != nil {
		defer claims.Close()
	}
	handler, err := controlhttp.New(controlhttp.Config{AllowedHosts: []string{listener.Addr().String(), "localhost:" + port},
		BrowserOrigin: os.Getenv("P11_BROWSER_ORIGIN"), InsecureLoopback: os.Getenv("P11_INSECURE_LOOPBACK") == "1", CursorKey: cursorKey,
		Sessions: identity.Sessions{DB: db, Provider: provider, PepperVersion: "auth_v1", Peppers: map[string][]byte{"auth_v1": authPepper}, IdentityPepper: identityPepper, TTL: 12 * time.Hour},
		Keys:     identity.KeyService{DB: db, Clock: clock.System{}, PepperVersion: "api_v1", Peppers: map[string][]byte{"api_v1": keyPepper}, MaxTTL: 30 * 24 * time.Hour},
		Market:   market.Catalog{DB: db}, Trade: trade.Service{DB: db}, Seller: seller.Service{DB: db, Fees: fees},
		Devices: device.Service{DB: db, PepperVersion: "pair_v1", Peppers: map[string][]byte{"pair_v1": pairPepper}},
		Claims:  claims,
		Routes:  routecontract.Service{DB: db, PaymentEnvironment: os.Getenv("P11_PAYMENT_ENVIRONMENT")}})
	if err != nil {
		return err
	}
	server := handler.NativeHTTPServer(listener.Addr().String())
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-signalCtx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if server.Shutdown(shutdownCtx) != nil {
				_ = server.Close()
			}
		case <-done:
		}
	}()
	fmt.Println("Native loopback control API listening at " + listener.Addr().String() + "; no model or payment execution enabled.")
	err = server.Serve(listener)
	close(done)
	<-shutdownDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func main() {
	if err := run(); err != nil {
		// Do not print errors that could contain a DSN, code, token or PEM.
		fmt.Fprintln(os.Stderr, "Control API stopped: check explicit identity configuration, native database migration and loopback settings.")
		os.Exit(1)
	}
}
