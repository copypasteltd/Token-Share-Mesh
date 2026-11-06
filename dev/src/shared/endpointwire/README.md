# Public endpoint wire metadata

This package validates public `P11_ENDPOINT_PUBLIC_1` bundles and old-identity-signed `P11_ENDPOINT_ROTATION_1` transitions. `SigningMessage` exposes domain-separated canonical PUBLIC bytes only. The client vault aliases these types; signed wire fields and domains remain unchanged.

It imports no endpoint vault, native cipher, private-key store, inference backend or runtime engine. A valid signature is not an independent trust pin, content-key possession proof, hardware attestation or business execution authority. Explicit historical public-key purpose reuse is rejected.

Source-only tests and interoperability vectors remain uncompiled/unexecuted in R026 CODE_FIRST. See [ER constraints](../../../../docs/技术开发/附录-账户设备公钥目录与原请求发布源码.md).
