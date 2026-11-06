# Account/device public endpoint directory

SOURCE_ONLY_UNVERIFIED / R026. `Service` registers public self-signed endpoint metadata under a current real account session or the original device request proof. It has no content private keys, cipher backend, payment or execution issuer.

`Register`, `Rotate`, `Revoke`, `Get`, `List` and `GetForOrder` use the original PostgreSQL store. Forward migration022 holds immutable public history and globally unique public-key purpose claims. Current credential and role checks precede replay; idempotency persists only the original recipient reference and rereads current status. A previous ACTIVE response cannot undo revocation.

Original provider device identity, original buyer/order/seller and frozen device bindings remain mandatory. A directory reply requires independent endpoint trust and does not prove content-key possession or grant an E2E channel. The existing plaintext route/lease restrictions stay in force.

No compile/test/migration/runtime has run. Full design and remaining work: [ER constraints](../../../../docs/技术开发/附录-账户设备公钥目录与原请求发布源码.md).
