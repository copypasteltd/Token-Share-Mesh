# Original-order funding guard

CODE_FIRST / SOURCE_ONLY_UNVERIFIED. This package is not a wallet, payment
adapter, budget reservation, trusted-usage source, ledger or execution grant.
Its production consumers are route contracts, rental funding and new scoped
API keys. All source and SQL remain uncompiled/unexecuted.

`CurrentTx` requires an existing READ COMMITTED transaction. It first takes the
original order's SHARE lock in a separate statement, then reads the unique
`order_fundings` allocation and original verified receipt in a fresh snapshot.
Machine refund writers serialize through that same order. Production writers
must acquire order -> payment before touching refund rows; the database trigger
also blocks direct inserts against active readers. Existing callers retain
their stronger original order lock when they already own one.

A historical `SETTLED` flag, arbitrary channel reference, nonempty profile,
`SYNTHETIC_TEST` record or a different order is insufficient. The guard matches
the original buyer, seller, contract hash, payment purpose, environment, amount,
binding/scope hashes and archived verified-paid receipt. It then rechecks both
current parties, approved seller, four independent reviewer proofs and the
original sale gate. Any nonfailed refund, closed order, expired deadline or
revocation blocks use. Returned validity is clipped to current approval/role
expiry as well as the original allocation deadline.

`payments.Machine.RecoverFundingOne` owns initial allocation. Verified payment
enqueues a dedicated metadata admission job in the receipt transaction. Under
the original order/payment locks, original current qualification, zero-fee
profile, unoccupied budget and deadline are checked before the first write.
Allocation, READY, order outbox and job completion commit together. A refused
allocation preserves SETTLED; a closed/expired original obligation goes to
metadata review. No path repeats precreate, charges, refunds, starts a model or
releases resource owners. The same allocation may be checked for multiple
original calls, but each call still needs its own budget and execution checks.

The current source supports only the explicitly registered
`SELLER_DIRECT_NO_FEE` payment profile. Fee profiles, paid renewals, qualified
refund/payout channels, trusted token/hour metering, acceptance and ledger remain
full-scope work. Legacy positive fixtures that used direct READY/SETTLED flags
must be replaced with qualified source fixtures; production has no test bypass.

Detailed constraints and the unexecuted test inventory:
[GF-001—040](../../../../docs/技术开发/附录-原订单资金唯一分配与当前准入源码.md).
