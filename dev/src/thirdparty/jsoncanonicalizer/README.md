# Pinned JCS source / 固定规范化源码

Unmodified Go source from cyberphone/json-canonicalization, commit
32a48195b9b20fe67b1795e39e4509f14d860b45, directory
go/src/webpki.org/jsoncanonicalizer. The original copyright headers and
Apache-2.0 license notice are retained byte for byte. Do not run formatting or
newline conversion on these upstream files.

Both upstream Go files include trailing spaces. Their exact-path Git whitespace
attributes disable only blank-at-eol reporting; they do not rewrite those bytes
or exempt our wrapper, tests, documentation or other source files. The repository
still uses `* -text` to preserve all recorded file hashes and migration bytes.

固定上游源码原字节保留；本项目包装器位于 shared/canonicaljson，先限制长度/深度、
拒绝重复键和非规范线字节，再调用上游实现。源码、许可证摘要记录在
client/crypto-gateway/dependencies.lock.json。

Current state: source only, not compiled or tested in R026. This pin is not a
claim of cryptographic review or completed interoperability.
