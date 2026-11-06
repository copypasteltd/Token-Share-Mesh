package payments

import (
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
)

type Refund struct {
	ID, Payment, Order, OriginalRequestKey string
	Amount                                 money.Fen
	State                                  string
}

// RefundAuthority is an independently derived cancellation/after-sale policy
// decision. It is not a buyer-supplied amount or a channel refund capability.
type RefundAuthority struct {
	Payment, Order, Reference, EvidenceHash string
	Maximum                                 money.Fen
}

// HeldRefunds counts successful, in-flight AND unknown original refunds. It
// refuses damaged/duplicate records instead of clamping them into free credit.
// The caller must lock the original payment and rows in one serializable write.
func HeldRefunds(payment Snapshot, rows []Refund) (money.Fen, error) {
	if !payment.Scope.Valid() || payment.Amount < 1 || payment.Amount > 5000 || len(rows) > 4096 {
		return 0, ErrConflict
	}
	idsSeen := map[string]bool{}
	keysSeen := map[string]bool{}
	var held money.Fen
	for _, r := range rows {
		if !ids.Valid(r.ID) || r.Payment != payment.Scope.Payment || r.Order != payment.Scope.Order || !merchantNumber.MatchString(r.OriginalRequestKey) || r.Amount < 1 || r.Amount > payment.Amount || idsSeen[r.ID] || keysSeen[r.OriginalRequestKey] {
			return 0, ErrConflict
		}
		idsSeen[r.ID] = true
		keysSeen[r.OriginalRequestKey] = true
		switch r.State {
		case "FAILED":
			continue
		case "REQUESTED", "PROCESSING", "SUCCEEDED", "UNKNOWN":
		default:
			return 0, ErrConflict
		}
		var e error
		held, e = money.Add(held, r.Amount)
		if e != nil || held > payment.Amount {
			return 0, ErrConflict
		}
	}
	return held, nil
}

// ReserveRefund returns the same old record on replay, including UNKNOWN. It
// never starts a channel request or treats unpaid closure as a refund. An
// approved actual channel capability is still required before admitting a SKU.
func ReserveRefund(payment Snapshot, rows []Refund, authority RefundAuthority, id, key string, amount money.Fen) (Refund, bool, error) {
	if payment.State != "SETTLED" || !transactionNumber.MatchString(payment.ChannelReference) || !hashPattern.MatchString(payment.EvidenceHash) ||
		authority.Payment != payment.Scope.Payment || authority.Order != payment.Scope.Order || !reference.MatchString(authority.Reference) || !hashPattern.MatchString(authority.EvidenceHash) ||
		authority.Maximum < 1 || authority.Maximum > payment.Amount || !ids.Valid(id) || !merchantNumber.MatchString(key) || amount < 1 {
		return Refund{}, false, ErrConflict
	}
	held, e := HeldRefunds(payment, rows)
	if e != nil {
		return Refund{}, false, e
	}
	for _, old := range rows {
		if old.ID == id || old.OriginalRequestKey == key {
			if old.ID != id || old.OriginalRequestKey != key || old.Amount != amount {
				return Refund{}, false, ErrConflict
			}
			return old, true, nil
		}
	}
	if held > authority.Maximum || amount > authority.Maximum-held {
		return Refund{}, false, ErrConflict
	}
	return Refund{ID: id, Payment: payment.Scope.Payment, Order: payment.Scope.Order, OriginalRequestKey: key, Amount: amount, State: "REQUESTED"}, false, nil
}

type Reconciliation struct {
	Classification                         string
	ApplyOriginalObservation, BlockNewWork bool
}

// Reconcile compares independently authenticated channel facts against this
// exact original scope. It does not modify history, repair a different order,
// trigger a payout or reduce refund obligations on an uncertain observation.
func Reconcile(payment Snapshot, observation Observation, executionDeadlinePassed bool) (Reconciliation, error) {
	if !payment.Scope.Valid() || payment.Scope != observation.scope || payment.Amount != observation.amount {
		return Reconciliation{"MISMATCH", false, true}, ErrConflict
	}
	if observation.kind == "UNRESOLVED" {
		return Reconciliation{"STILL_UNKNOWN", false, true}, nil
	}
	next, e := Merge(payment, observation)
	if e != nil {
		return Reconciliation{"MISMATCH", false, true}, e
	}
	if next.State != "SETTLED" {
		return Reconciliation{"NOT_CONFIRMED_PAID", false, true}, nil
	}
	if !observation.IsSettled() {
		return Reconciliation{"PRESERVE_EXISTING_PAID_FACT", false, true}, nil
	}
	if executionDeadlinePassed {
		return Reconciliation{"LATE_PAID_RESOLUTION", true, true}, nil
	}
	return Reconciliation{"CONFIRMED_ORIGINAL_PAID", true, false}, nil
}
