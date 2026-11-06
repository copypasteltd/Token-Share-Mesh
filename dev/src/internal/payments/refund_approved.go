package payments

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

// Callers must authenticate the actual actor, lock the original parent/payment
// and hold a current independent decision. No synthetic buyer is constructed.
// Both trusted original-decision consumption and public reviewed applications
// share this exact financial transaction; neither calls the channel here.
func (s *RefundService) createApprovedRefundTx(ctx context.Context, tx pgx.Tx, o machineOrder, paid machineRecord, original Registration, r RefundRegistration, id, decisionRef, decisionHash string) (OriginalRefundView, error) {
	if e := requireRefundRouteTx(ctx, tx, o, original, r, true); e != nil {
		return OriginalRefundView{}, e
	}
	authority, amount, decision, e := refundDecisionTx(ctx, tx, o, paid, original, r, decisionRef, decisionHash)
	if e != nil {
		return OriginalRefundView{}, e
	}
	if e = refundCapacityTx(ctx, tx, o, paid, authority.Maximum); e != nil {
		return OriginalRefundView{}, e
	}
	rows, e := originalRefundsTx(ctx, tx, paid.Snapshot)
	if e != nil {
		return OriginalRefundView{}, e
	}
	number, e := NewOutTradeNo()
	if e != nil {
		return OriginalRefundView{}, e
	}
	reserved, _, e := ReserveRefund(paid.Snapshot, rows, authority, id, number, amount)
	if e != nil {
		return OriginalRefundView{}, e
	}
	snapshot, receipt, e := s.reverifyOriginalPaidTx(ctx, tx, paid, original, "")
	if e != nil {
		return OriginalRefundView{}, e
	}
	before := o.Now.Add(10 * time.Minute)
	if r.Channel.route.ValidUntil.Before(before) {
		before = r.Channel.route.ValidUntil
	}
	intent, e := NewRefundIntent(snapshot, reserved, authority, r.Channel.route, paid.Binding, o.Now, before)
	if e != nil {
		return OriginalRefundView{}, e
	}
	return insertRefundTx(ctx, tx, o, paid, r, intent, decision, receipt)
}
