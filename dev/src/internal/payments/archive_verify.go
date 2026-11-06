package payments

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"

	"p11.local/compute/shared/strictjson"
)

// ReverifyPaidReceipt does not send a payment/proof/channel request. It checks
// the COMPLETE original signed archive again under this retained original
// registration; decrypting a Vault blob alone cannot create this observation.
// The caller owns the original database payment/receipt consistency check.
func (a *Alipay) ReverifyPaidReceipt(ctx context.Context, i Intent, paid Snapshot, raw []byte) (Observation, error) {
	if a == nil || ctx == nil || ctx.Err() != nil || !a.binding.ResolvableAt(a.now()) || !i.BoundTo(a.binding) ||
		paid.Scope != i.Scope || paid.State != "SETTLED" || paid.Amount != i.Amount || !transactionNumber.MatchString(paid.ChannelReference) ||
		!hashPattern.MatchString(paid.EvidenceHash) || paid.ObservedAt.IsZero() || paid.ObservedAt.Before(i.CreatedAt) ||
		len(raw) < 2 || len(raw) > MaximumChannelResponse {
		return Observation{}, ErrProof
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return Observation{}, ErrProof
	}
	a.operations.Add(1)
	a.mu.Unlock()
	defer a.operations.Done()
	var envelope map[string]json.RawMessage
	if strictjson.Decode(raw, &envelope, MaximumChannelResponse) != nil || len(envelope) != 2 {
		return Observation{}, ErrProof
	}
	node := envelope[strings.ReplaceAll(verifyMethod, ".", "_")+"_response"]
	var signature string
	if len(node) < 2 || node[0] != '{' || json.Unmarshal(envelope["sign"], &signature) != nil {
		return Observation{}, ErrProof
	}
	signed, e := base64.StdEncoding.Strict().DecodeString(signature)
	defer clear(signed)
	sum := sha256.Sum256(node)
	if e != nil || hex.EncodeToString(sum[:]) != paid.EvidenceHash || rsa.VerifyPKCS1v15(a.public, crypto.SHA256, sum[:], signed) != nil {
		return Observation{}, ErrProof
	}
	var fields map[string]json.RawMessage
	if strictjson.Decode(node, &fields, MaximumChannelResponse) != nil {
		return Observation{}, ErrProof
	}
	var code, trade, number, amount string
	var active bool
	if json.Unmarshal(fields["code"], &code) != nil || code != "10000" || json.Unmarshal(fields["trade_no"], &trade) != nil || trade != paid.ChannelReference ||
		json.Unmarshal(fields["out_trade_no"], &number) != nil || number != i.Scope.OutTradeNo || json.Unmarshal(fields["amount"], &amount) != nil ||
		json.Unmarshal(fields["active"], &active) != nil || !active {
		return Observation{}, ErrProof
	}
	fen, e := ParseYuan(amount)
	if e != nil || fen != paid.Amount || ctx.Err() != nil || a.life.Err() != nil {
		return Observation{}, ErrProof
	}
	return Observation{scope: i.Scope, amount: fen, kind: "VERIFIED_PAID", trade: trade, evidenceHash: paid.EvidenceHash, observed: paid.ObservedAt, receipt: newReceiptState(raw)}, nil
}
