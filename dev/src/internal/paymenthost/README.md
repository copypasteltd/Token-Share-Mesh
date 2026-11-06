# Native original-payment host / 原生支付宿主

`CODE_FIRST / SOURCE_ONLY_UNVERIFIED`. This package loads an explicit original
personal-seller registry under a trusted runtime root, independent RSA pins and
secrets, and separate proof/receipt keyrings. It owns one dedicated same-proof
recovery loop and logical expired-row retirement. Construction performs no RPC,
approval, migration, listener startup or funds operation.

The API owns its Host and exposes only the original authenticated create/get
operations. A separate `scheduler/cmd/p11-payment-worker` owns its native loop.
Both must use the same explicit environment and retained original registration;
OAuth configuration is never an implicit payment registration. Missing API
configuration disables collection; partial configuration fails startup.

Close cancels and joins the actual loop, Machine operations and channel calls
before releasing either keyring. The outer app closes the DB afterward. A caller
deadline retains ownership and keys until a later successful Close. Live-row
retirement does not erase WAL/backups or resolve financial obligations.

All source remains unbuilt/unexecuted. SDK/A402/Proof and actual PayLauncher,
unique usable funds/business admission, full qualified refund/payout/fee/
statement/renewal/metering/ledger and actual secret/merchant/device evidence are
still open; this package is not a delivered complete payment system.

完整 PH-001—040、环境变量、注册/钥环字段与使用顺序见
[原生宿主与本人接口附录](../../../../docs/技术开发/附录-MachinePay原生宿主与本人支付接口源码.md)。
全部生产代码、实际依赖接线和测试源码齐备后才统一验证；本次未运行任何服务、渠道或资金操作。
