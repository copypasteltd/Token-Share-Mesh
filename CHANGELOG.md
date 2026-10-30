# Changelog

## R022 native result custody and protocol output

Original native output now moves into a separate AES-256-GCM result store before compute cleanup. Defaults reserve 16 × 8 MiB for five minutes. Synchronized original compute can release its GPU while its exact order/local-generation result remains readable. Process-only keys, one reader, reserved quotas and expiration bound custody; key loss or ambiguous submission never reruns inference. PIPE_2 adds actual input counts and the matched original stop index.

An authorized original result renders OpenAI Chat, Responses or Anthropic Messages as JSON/SSE with UTF-8, usage options, stop semantics and bounded writes. Defaults are 512 KiB JSON and 64 KiB per event. Responses repeats final text and needs explicit capability budgeting before public activation; overflow cannot emit a false success tail. A local write Receipt is not buyer ACK, trusted usage or payment evidence.

This is a PLAINTEXT/STANDARD source subset, not E2E/LP. Node CLI execution stays disabled. Seventeen new regular Go cases bring the manifest to 275; six separately tagged actual Windows worker/synthetic C ABI cases include encrypted custody and all three protocol outputs. Real models/GPUs, a separate execution identity, independent official SDK certification, public result transport, funds and full acceptance remain pending. The early supervisor shutdown timeout and first tagged failure are preserved; later passing runs do not resolve historical timing risks.

[RC-001—060](docs/技术开发/附录-原生文本结果加密保管与标准协议交付.md) · [R022 evidence](dev/tests/evidence/R022-验证报告.md) · [All 56 tasks](dev/progress/R022-本轮实现与完整待办.md).


## R021 — Replaceable native LLM worker and bounded pipes

- Add a frozen v0.8.0 llama.cpp C API worker and Go InputAdapter with independently approved pack/model/template/physical-resource hashes. Tasks cannot select local paths, shells or download URLs.
- Preserve three strict text input profiles, template/tokenizer calls, sampling/stop behavior, reusable model weights, separate cancellation, two 64-KiB output buffers and original-owner synchronized cleanup. Null context initialization and process exits remain UNKNOWN; late completion cannot undo revocation.
- Add owned Windows process/handle boundaries and Linux Go compilation. Five actual Windows-process tests use a synthetic C ABI DLL; they are separate from real model/GPU/LP/E2E/trusted-usage acceptance. Node CLI execution remains disabled; the long-running host and public SDK output/ACK remain open.
- Update NK-001—042, bilingual usage/README, actual UI/reader media, eight IMP items and all 56 tasks/222 open steps. Preserve all 22 published SQL files byte-for-byte, deferred DEV-55 and the ACTIVE delivery goal.
- Retain compilation errors, the initial synthetic ABI three-fail/two-pass run, source-build timeout and pre-hardening full regression. All 18 first-run browser assets are archived by exact receipt path/hash. Historical R016/R019/R020 timing causes and the 19 missing R020 historical PNGs remain disclosed.

- The second full run failed only at the Web pairing response-body read through CDP. Preserve actual failed stdout and all five matching screenshots in a supplemental manifest; the initial archive followed a stale pass receipt. The browser fixture now captures the actual upstream response before fulfillment, without changing product behavior, assertions or time limits.

- A subsequent full run recorded two native shutdown/deadline timing failures, expiry of the unchanged shared 220-second integration context, and a 12-second H5 narrow logout wait. The actual screenshot and raw failures are retained. The two unchanged native cases passed three isolated repetitions each; underlying timing causes remain open and all original bounds stay in force.

[Current evidence](dev/tests/evidence/R021-验证报告.md) · [Development attempts](dev/tests/evidence/R021-development-attempts.json) · [All remaining work](dev/progress/R021-本轮实现与完整待办.md)

## R020 · 2026-10-31 · Native execution host / 原生执行宿主

R020 adds a persistent native execution host with an independently provisioned issuer public pin and original order/kernel approval. The original claim is encrypted and committed before networking; missing replies and restarts retain that exact command. Bounded manifest/input reads produce an in-memory ticket. An explicit InputAdapter hands original bytes to the existing once-only native supervisor; an old receipt cannot submit content again. Cleanup is projected only from the original execution journal with the same lease and metadata hash. Corrupted selectors cannot hide unfinished commands, and missing or mismatched anchored state is never recreated. Windows reuses actual DPAPI/private ACLs; Linux remains cross-built with protocol fixtures. CLI host-init/host-status do not install or run models; execution_enabled remains false. This covers three PLAINTEXT/STANDARD text profiles. Metadata encryption does not provide content E2E or provider blindness. Real tokenizers/kernels, output/usage/ACK/renewal, payment channels, LP/PE/rental and complete acceptance remain open. The 37/95 control-operation and 65-table/664-column/113-FK counts are unchanged. Previously published SQL001–010 and execution DDL bytes are preserved; a separate native host SQLite schema is new. All 56 tasks/222 development steps and unresolved R016/R019 slow-metadata timing risks remain open; the goal stays ACTIVE.

15 new top-level cases; full native/browser/build results are recorded separately. Failed attempts remain visible; this is source-subset progress, not real hardware/channel or whole-product acceptance.

The first R020 complete run passed 177 / failed 73 / skipped 0. Migration 001 in a legacy-upgrade child DB hit the existing statement timeout; a contemporaneous checkpoint reported 110.501s sync, and later cases exhausted the unchanged 220s package context. The H5 journey separately terminated after 237.63s without a final receipt; its failure screenshot is retained. Underlying storage and browser causes remain unproven. Validation now runs independent native tests before starting PG, explicitly observes enabled/checked Taro consent, and journals failure evidence earlier. Product source, published SQL, existing assertions and time bounds are unchanged. Later passing runs do not establish timing stability or erase these failures. [Full failure archive / 完整失败归档](dev/tests/evidence/R020-full-attempt1-archive.json)。

The second R020 run completed only the native segment: 113 passed / 2 failed / 0 skipped. Two existing supervisor cases returned ErrShutdown within the unchanged cleanup bound; the failed run did not independently observe final disk ownership. Both database test executables compiled, but the runner incorrectly expected test2json to be a standalone executable and stopped during preparation. PostgreSQL and both browser segments never started. The runner now uses the pinned Go tool entry; product Go source, assertions, SQL and runtime deadlines are unchanged. Original events, the runner and build digests are retained. A later pass does not establish cleanup timing stability. [Partial failure archive / 部分失败归档](dev/tests/evidence/R020-full-attempt2-archive.json)。

Five legacy-migration tests in the fourth complete R020 run failed because the precompiled-test runner did not preserve go test package working directories; their fixture could not read the five real pre-006 SQL files. The test-launch cause is identified and corrected by restoring package working directories, without changing fixtures, product source or published SQL. This is separate from the first-run SQL timeout and second-run supervisor cleanup failures, whose underlying causes remain unproven. [Failure archive / 失败归档](dev/tests/evidence/R020-full-attempt4-archive.json)。

The fifth complete R020 run passed 249 / failed 1 / skipped 0. Native 115 cases, database core and Web passed; H5 page interactions also passed and the browser closed, but the test-only multi-device heartbeat UPDATE deadlocked with the market FOR SHARE transaction (40P01), reported by test cleanup. Both waits and the batch statement are retained. Synthetic heartbeats now use independent per-device autocommit statements, preserving the 5s tick, real 45s freshness, every fixture device and all error assertions, without retries or ignored failures. This fixture correction does not prove production deadlocks absent or resolve earlier unproven cleanup/storage/H5 timing causes. [Failure archive / 失败归档](dev/tests/evidence/R020-full-attempt5-archive.json)。

The historical attempt 4/5 archiver omitted nested media directories. Exact original SHA-256 matches recovered 8/18 and 9/18 assets; all eight historical MP4/GIF files re-encoded from retained original frames match their original digests. Nineteen original PNG files are unavailable and individually listed. The final 14 frontend screenshots, four frontend MP4/GIF files and reader assets are complete. Missing historical files are not used as current verification evidence. [Recovery inventory](dev/tests/evidence/R020-historical-media-recovery.json)。


## R019 · 2026-10-30 · Restricted original input / 原清单与输入受限交付

R019 adds restricted manifest and exact original input delivery for an already claimed lease: 37/95 explicitly registered control operations, 65 tables / 664 columns / 113 foreign keys. INT-094/095 are added to the authoritative contract and permanent registry first. Both GETs require the original signed device and recheck its original buyer key/route/order, same-payment funds, qualification, full existing hold, device version, input, physical owner, both fences and unchanged expiry using the shared order-first lock sequence. The delivery service has no issuer private key. Input requires original AES-GCM/AAD, separately keyed versioned HMAC and same-profile Preflight, then returns the original bytes. The native receiver requires an independent issuer pin and original order/route/mode plus locally approved kernel facts; it neither approves nor starts a kernel. This is a trusted PLAINTEXT/STANDARD authenticated transport hop: a ciphertext digest is not a signed plaintext commitment, E2E or provider blindness. Reads never ACK execution, renew, reassign, complete delivery jobs, bill or release a resource. Published 001–010 and client SQLite bytes remain unchanged. Durable host commands, real tokenizer/kernels/usage/channel, E2E/LP/PE/rental and complete acceptance remain open.

12 new top-level cases; full verification is recorded separately. All 56 tasks / 222 open development steps and the unresolved R016 timing risk remain.

The first R019 complete run passed 233 / failed 1: the existing TestSupervisorMetadataStepsHaveIndependentBudgets returned ErrShutdown under injected slow metadata. That failure did not authorize release; final disk ownership was not independently observed in that failed run. Exact original logs and 18 media files are archived. The standalone diagnostic emitted no test events before its outer build/process timeout, so it has no passing result. The second complete native/browser attempt repeated unchanged source, assertions and runtime bounds; the subsequent frozen-registry correction is documented separately. A later pass does not establish the cause or timing stability; DEV-53 remains open.

The second R019 complete run also passed 233 / failed 1: the slow-metadata case passed, while the existing unknown-registry case could not read its expected occupied snapshot after shutdown. Exact logs, source evidence and 18 media files are separately archived. A subsequent correction preserves already quarantined/stopping/cleaning owners without repeated writes when a frozen kernel binding is absent, visits every resource before reporting incomplete shutdown, and retains bounded cancellation for other matching original owners. A mixed-resource regression was added without relaxing existing assertions or bounds. This does not establish the cause of the R016 or first R019 slow-metadata failure; DEV-53 remains open.

Both READMEs now derive screenshot, MP4 and GIF links from the current implementation round; a repository check rejects stale-round product media. Current captures are R019.

## R018 · 2026-10-30 · Native original-lease claim / 原设备签名领取

R018 adds authenticated native delivery of the original signed lease: 35/93 registered control operations and 65 tables / 664 columns / 113 foreign keys. Only the original device proof can claim; its original command ID equals Idempotency-Key and is permanently bound to the same lease by forward migration 010 with one atomic safe event. Every recovery rechecks the original scoped buyer key/route, order and same-payment funds, eligibility, full existing hold, encrypted input metadata, physical owner, both fences and unchanged expiry. The delivery service has explicit public verifiers only: it cannot decrypt, issue, renew, reassign, ACK execution, complete delivery tasks or bill. Generic metadata workers can claim or exhaust only their supported kinds, leaving execution-delivery intents untouched. Independent device pins/approved plans, input/manifest transport, durable host execution commands, real kernels/usage/channel, E2E/LP/PE and rental remain open. Published 001–009 and client SQLite bytes are unchanged; history stays UNBOUND_CLAIM.

18 new top-level cases; current full 222-case native/browser verification and six builds are recorded separately. The first 16-case subset and first full run (221 passed / 1 failed) are archived. The full-run failure identified a generic queue kind-isolation defect, corrected with stronger no-mutation assertions. R016 shutdown timing risk remains unresolved. All 56 tasks / 222 open development steps remain.

## R017 · 2026-10-30 · Native input and initial lease / 原生输入与首租约

R017 connects internal native-text input and initial lease admission: the actual body is strictly validated and stored with AES-256-GCM. The original order, current scoped key/qualification, same-order funding and existing full per-call hold are rechecked before an explicitly FREE native-device resource, dual fences, metadata manifest, signed first lease (at most 45 seconds within the original hard deadline), safe event and durable delivery intent commit atomically. Original-key recovery never replaces input/owner or renews authority; expiry, revocation and unknown outcomes retain budget and ownership. This protects PLAINTEXT storage only, not customer E2E or provider blindness. Model listeners, actual claim/content delivery, production secrets/independent trust, real engines/usage/payments and LP/PE remain pending. Forward009 adds seven columns/one FK; 001–008 and native SQLite DDL retain their bytes. The scope remains 34/93 operations and 65 tables. Platform generations stop at the persisted signed-bigint limit.

20 new top-level cases; current 204-case full native/browser run and six builds are reported separately. Earlier failed attempts are retained. 56 tasks / 222 open steps remain; no full product acceptance.

## R016 — Original-call preparation, call slots and atomic order capacity

- Add the internal ROUTE_PREPARE_1 service. Under the original order write lock, recheck the current scoped key, route, same-order funding and qualification, then commit INPUT_PENDING, the full per-call hold, a durable call slot and a four-field safe event together.
- Recover the original job/reservation with the original key; changed body, safe parameters or API key conflict. Current identity revocation blocks replay. An inactive route/funding/device allows only recovery of the original record; cancellation, expiry and unknown results retain holds and slots.
- Persist only a domain-separated versioned HMAC, never the body or raw request digest. Copy configured keys, retain original versions for recovery and fail closed if missing. Production secret loading and full lifecycle remain pending.
- Bound preparation by the shortest original route/order, current funding/qualification and API-key deadline. Require the frozen maximum input plus requested output ceiling to fit the per-call cap; never infer actual usage.
- Add forward-only 008: six columns, three FKs, two indexes and deferred budget/event guards. Preserve 001–007 and native SQLite DDL bytes; historical UNBOUND jobs gain no invented consent.
- Register 14 native regression cases; see the R016 report for the current full result. Refresh real browser/native-control media, bilingual docs and all 56 tasks/222 open steps. Retain the early progress-generation rejection for missing evidence and generate only after the evidence exists.
- Keep 34/93 control operations and 65 tables. Preparation grants no Lease, relay Grant or inference authority. Input-ready/resource admission, trusted usage/funds, production engines, E2E/LP/PE, route UI and whole-product acceptance remain pending. No virtualization, deployment or observability work.
- Preserve the first full run (183 passed/one existing renewal-cleanup failure) and three unchanged isolated passes. The cause remains unproven and DEV-53 timing stability stays open.

## R015 — Confirmed order routes and scoped SDK model resolution

- Add three owner-session control operations with explicit original-order/payment binding, immutable seller/listing/model/profile/price limits, current-state replay and bounded signed pagination.
- Resolve exactly one key-bound model/protocol in a SQL transaction, including revoked-key/current-role checks; ambiguous or unavailable routes never select another seller. Configuration is not execution admission.
- Add forward-only 007 and its matching manifest: five columns, original-payment FK, one confirmed route per order, immutable bindings and conservative legacy quarantine. Keep 001–006 and native SQLite DDL unchanged.
- Replace the stale API migration-version check with read-only embedded name/hash/set verification. Add 14 original-order/SDK/upgrade/HTTP cases, preserve failed fixture attempts and refresh evidence/media and bilingual documentation.
- Keep real channel verification, qualified usage, invocation/call accounting, production engines, E2E and route UI pending; all 56 tasks and 222 open steps remain tracked. No virtualization, deployment or observability work.
- Preserve sold listing revisions when sellers publish new versions; new purchases remain current-version-only. Correct the short real-lease regression to distinguish zero-submit safe expiry rejection from one deadline-bounded submission, retaining its 150ms/800ms limits and all failed attempts.

## R014 — Order capacity reservations and qualified consumption

- Add SQL-only reserve/complete/lookup/snapshot transactions bound to the original order, buyer, seller, contract and purpose. They compose with caller intents and never authorize payment or execution.
- Update the parent order atomically; enforce capacity and immutable bindings for service and direct SQL writers, including stale repeatable-read snapshots.
- Consume only existing immutable trade qualifications linked to original verified usage, accepted delivery or final rental meter. Caller amounts, unknown usage and disconnected streams cannot release holds.
- Add forward-only 006 with eight columns and conservative legacy occupancy; inconsistent historical totals abort the entire upgrade. Keep 001–005 and native SQLite DDL byte-identical.
- Register 16 native budget/concurrency/upgrade cases and strict 156-case full evidence; isolate core/Web/H5 processes and browser fixture databases without extending deadlines, and retain failures separately. Refresh current browser/native CLI media and exact-hash reused frontend receipts, bilingual docs and all 56 tasks/222 open steps.
- Keep 31/93 control operations and 65 tables; funding/route admission, real qualified-source generation, engines, payments and E2E/LP/PE acceptance remain pending. No local virtualization or deployment/observability work.

## R013 — Native metadata and execution deadlines

- Make metadata waiters cancellable; cancellation before SQL preserves the healthy original-owner ledger. SQL/commit ambiguity still closes the gate.
- Anchor native execution deadlines before transaction work; return no expired authority and give adapters the fixed deadline rather than restarting relative remaining time.
- Add per-operation MetadataTimeout (2s default, 20ms–5s) independently of backend CallTimeout; cap shutdown I/O by its absolute deadline. Signed lease and backend limits are unchanged.
- Avoid rewriting known backend mappings and already-persisted stop phases; keep full stopped/cleanup evidence and owner CAS.
- Wake shutdown after durable cleanup and slot release, without extending its deadline; retain the deterministic FREE-yet-timeout regression.
- Retain five initial controlled failures and intermediate fixture/I/O failures. Update strict 140-case registration, both current browser/native-binary journeys, exact-source reused frontend receipts, NMD-001–013, bilingual docs/media and all 56 tasks/222 pending steps.
- Keep 31/93 operations, 65 platform tables, byte-identical 001–005/local DDL and deferred deployment/observability. No local virtualization, GPU inference, real payment or content E2E acceptance.

## R012 — Bound native protocol relay

- Follow-up: replace delimiter-based substring checks with exact field-name equality in request/response allowlists; preserve pre-fix failures. The 20 changed relay cases pass. Reuse 114 identical-source cases from the initial published full pass only after exact source/module/input and independent-package checks. The latest full rerun FAILED seven existing short-deadline cases; a serialized diagnostic FAILED six. Preserve both failures; host contention is an observation, not a proven cause. No deadline or cleanup rule was relaxed, and no current 134-case full pass is claimed.

- Fix a cleanup/sweep race: an already-completed owner snapshot is skipped on its rejected owner CAS, preserving replacement owners and real cleanup gates.

- Add immutable seller/contract/route bindings and three native text JSON/SSE profiles with exact field, Unicode and header validation.
- Validate independent Chat, Responses and Messages lifecycles, including Responses text integrity; never invent completion or usage.
- Require explicit trusted admission/secret ports, separate plaintext domains, disable proxies/redirects/rotation/POST replay and bound cancellation, deadlines and backpressure.
- Add 21 Go cases (20 relay and one supervisor regression; 134 total) with owned native loopback backends and explicit authority fixtures; no production funding, SDK certification, physical engine stop or content E2E claim.
- Rerun both browser/current Windows-node journeys; reuse unchanged 21-case TS/three-target builds only after source/output/log hash checks. Update BRC-001–024, bilingual READMEs, actual media and all 56 tasks/222 pending steps.
- Keep 31/93 operations, 65 platform tables, byte-identical migrations 001–005 and deferred deployment/observability; no local virtualization.

## R011 — Native execution supervisor and original-backend recovery

- Add a running, bounded metadata supervisor with frozen environment/capability/manifest bindings, original one-shot submit/query, deadline/cancellation handling, stopped-backend cleanup retries and conservative shutdown.
- Couple native control drain/revocation to the same-node supervisor; pending or revoked control may recover old owners while new admission remains closed.
- Fix provider-profile loading when Web/Taro pages mount during original-request recovery; wait for idle and attempt once per owner, retaining explicit refresh after a read failure.
- Add 21 local Go cases (113 total). Engine observations and cleanup remain synthetic; real GPU engines, process isolation, content E2E, payment and full acceptance are pending. Rerun both browser/native-CLI journeys and update bilingual docs, screenshots/recordings and all 56 tasks/222 remaining steps.
- Keep platform migrations 001–005 byte-identical, 31/93 operations, 65 tables and deferred deployment/observability. No local virtualization.

[简体中文](CHANGELOG.zh-CN.md)

Entries use the same round IDs as the implementation tracker. No entry denotes a
general-availability platform release unless the full acceptance evidence says so.

## R010 · Native execution metadata inbox and physical-resource gate

Added strict signed execution-Lease verification, an independently approved-plan boundary, a once-only encrypted node anchor and a bounded CGO-free SQLite inbox. Original commands, uint64 job/resource high waters, local owner generations and backend mappings persist before submission; duplicates query the original request. Managed local work/jobs/maintenance share one physical gate. Signed revision renewals, absolute/monotonic expiry, clock rollback/reopen quarantine and stop/cleanup owner CAS cannot silently free or resurrect an owner.

Added 20 local Go cases (92 total), actual SQLite abrupt-process recovery and Windows DPAPI/SQLite/WAL/SHM ACL checks. Both current browser journeys run six real Windows commands including execution-init/status; no engine or resource qualification is enabled. Unchanged frontend cases/builds are verified by hashes and current captures; 31/93 operations, 65 platform tables and migrations 001–005 remain unchanged. SQLite's five local tables contain metadata only, not prompts/content keys or an encrypted content store. Real platform trust issuance, timers/adapters, rentals, content E2E, hardware/channels and full acceptance remain pending. All 56 tasks/222 open steps, bilingual docs, exact dependency/bundled notices and media provenance stay synchronized.

## R009 · Provider controls across both clients with original-command recovery

Connect the frozen NB04 React/Taro and React/Vite views to owner pairing, paginated devices, availability/disk/phone conditions, drain and identity revocation. Once-only credentials remain masked in page memory and are removed on hide/leave/expiry/authority loss; late replies cannot reintroduce them. Unknown writes keep the original method, key, body and strong ETag. Policy acknowledgement requires a fresh device read and grants neither execution nor sale qualification.

Verify 72 local Go cases, 21 shared TypeScript cases, three build targets and both real browser/native Windows control journeys. Preserve both the early asynchronous H5 response-read failure and a Web catalog/fixture-heartbeat snapshot conflict. Await the complete response and use the existing explicit market refresh for transient read 503s; never automatically retry writes. Each UI recovers the original pairing/PATCH, drains/revokes and checks that the native heartbeat is rejected after revocation. Publish 14 product screenshots and MP4/GIF provenance without credentials. Include R006–R009 public frontend build/test logs and verify both hashes and Git inclusion so a clone retains reviewable evidence. Still 31/93 operations and 65 tables, migrations 001–005 unchanged; retain all 56 tasks/222 steps and real-channel/hardware/content-encryption/full-acceptance gates.

## R008 · Native provider control agent and local recovery

Added foreground p11-node initialization, enrollment, original-command recovery, safe status, heartbeat and bounded outbound loop. Windows protects the state master with current-user DPAPI and owner/SYSTEM ACLs; Linux uses Secret Service and fails closed on missing/locked keys, with real Linux keyring validation still pending. An AES-GCM one-command journal, atomic replacement and OS writer lock precede network writes; restart retains the identity, original key and counter. Reports remain UNKNOWN and start no engine or automatic sale.

Added actual Windows DPAPI/ACL, process-termination recovery and native HTTP/PostgreSQL lost-enrollment/heartbeat-reply checks: 72 local Go cases, with two failed full attempts and their corrections retained. Still 31/93 operations and 65 tables; migrations 001–005 stay byte-identical. Unchanged frontend source reuses hash-verified 13-case/three-build receipts, with current backend browser/media regression. Bilingual docs and all 56 tasks/222 outstanding steps remain synchronized; full execution, engines, content encryption, real channels and 80 main acceptance cases remain incomplete.

## R007 · Provider identity and control

Added one-time pairing, original enrollment recovery, Ed25519 request proofs with durable nonce protection, monotonic heartbeats, strong-ETag resource policies and drain/revoke: 31/93 control operations. Registration starts with sharing off; heartbeats grant no qualification. Revocation preserves in-flight attempt owners/resource generations. Complete provider daemons, native key stores and execution remain pending.

Forward migration 005 adds DATA-065; migrations 001–004 retain their exact bytes. The catalog now has 65 tables. All 60 Go cases pass; 13 TS cases/three build receipts match unchanged frontend source, and both browser journeys were replayed against the current backend with independent captures. Temporary database and owned browsers closed. Bilingual documentation and all 56 tasks/222 remaining steps updated; 80 main and 96 full NB acceptance cases remain unexecuted. Source goal stays ACTIVE.

## R006 — 2026-10-30

- Implement React/Taro Alipay pages, React/Vite Web and shared TypeScript control logic using NB-1.0 Neubrutalism, concrete personal-device models and hourly/separate input-output M token prices.
- Add authoritative quotes, unchecked-by-default confirmation, owner WAITING_PAYMENT orders and read-only sharing. Preserve exact pending command keys/bodies through reload; discard stale owner/filter responses. Pairing/payment/settlement stay closed; no prompts are collected.
- Separate explicit native ALIPAY_NATIVE_NONCE_1 (server CSPRNG, digest-only, one-time) from Web S256 with temporary per-tab proof. Revise authoritative auth guidance; server nonce is not client-generated PKCE.
- Give migration admission a bounded 30s advisory-lock wait, then restore normal transaction limits; regress two migrators behind a real held lock. Preserve SQL001–004 bytes and reject scalar null aliases in strict control JSON.
- Pass 45 Go cases (including Web/Taro H5 browser subcases), 13 shared TS cases and Alipay/H5/Web builds. Retain failed attempts and their explanations. Each lost committed-order response recovers with its original command and leaves one order/event in PostgreSQL.
- Add ten actual product screenshots, two MP4s and corresponding GIFs with independent provenance. Close owned browsers and native temporary databases; use no local virtualization.
- Synchronize bilingual READMEs, constraints, lock metadata/declared licenses, evidence and all 56 tasks/222 pending steps. Seventeen in progress, 37 not started, one conditional, one deferred, zero fully complete; 80 main tests/96 full scenarios and real authorization/devices/money/crypto/performance remain unexecuted.

## R005 — 2026-10-30 — Seller submissions and immutable listing revisions

- Add account-owned seller applications and consent/withdrawal receipts; submitted claims never grant SELLER or current channel/device qualification.
- Add listing draft/read/update/submit/pause services with strong ETags, concurrent CAS, owner-bound signed pagination and immutable prior revisions/order contracts.
- Derive concrete device/environment bindings from owned rows; enforce approved regions, an explicit platform fee policy, current evidence and availability windows.
- Stop new supply atomically on seller exit while retaining money, orders and resource fences; independent SELLER can stop sharing without BUYER permission.
- Register 23/93 native control operations. Append forward 004 and DATA-063/064 for 64 current tables/613 columns, preserving 001–003 bytes.
- Pass 43 actual limited native cases plus Windows/Linux builds, retain the initial failure/corrections, and stop every temporary native database. Real review/channel/device/UI/full-product acceptance remains pending.
- Refresh bilingual usage/README, technical reader media, all 56 tasks and 222 remaining substeps; source goal stays ACTIVE.

## R004 — 2026-10-30 — Verified identity and native control HTTP

- Add RSA2 Alipay request signing and original-response verification, with explicit app/subject namespace and no unsigned identity fallback.
- Add one-use challenges, versioned sessions, BUYER-only registration, revocation and unknown-exchange closure; never persist original channel codes/tokens.
- Add 13 native HTTP operations for identity, keys, market, quotes and orders, driven by a manifest checked against 93 canonical operations.
- Enforce WEB Cookie/Origin/CSRF and MINIAPP Bearer separation, tenant checks, signed pagination, bounded JSON/limits and key-secret replay references.
- Append forward 003 (62 current tables) and INT-090—093/DATA-061—062 without rewriting old migrations or IDs.
- Pass 34 actual native cases, format/vet/dependency checks and Windows/Linux admin/API builds. Preserve the initial failure and correction; stop the temporary database. Real channel/UI/payment/provider acceptance remains pending.
- Synchronize bilingual guides, actual reader media and all 56 tasks/222 remaining substeps; keep the source goal active.

## R003 — 2026-10-30 — NB pricing and trade application services

- Continue the complete-source objective against the five Neubrutalism pages, retaining 48 refinements and every one of the 56 pending tasks.
- Add NB_PRICE_1 hourly and separate input/output M-token rates, exact cumulative calculation and retained-commission targets; reject lossy legacy-minute conversion.
- Add concrete C2C seller/device projections, scoped qualification/evidence/capacity checks, frozen quotes, atomic buyer orders and owner-only reads.
- Commit quote consumption, consent audit and outbox with each order; recheck current roles before replay, reject API-key purchases and implicit seller/privacy changes.
- Add forward migration 002 while preserving 001 and the legacy rental-minute column. Historical orders retain raw JSON; the historical quote reader remains unfinished.
- Pass 22 native cases, dependency/format/vet checks and Windows/Linux cross-builds; stop the temporary native database. Preserve the first duplicate-trigger migration failure and verify the correction in a fresh instance.
- Update machine contracts, implementation boundaries, both READMEs and numbered progress; refresh actual documentation-reader media. Product UI, public HTTP, physical devices and payment acceptance remain unfinished.

The earlier NB-R1 pause records its historical phase; R003 resumes source implementation. All six NB gaps remain open, and deployment/HTTPS/operations/observability stay deferred.

## NB-R1 — 2026-10-30 — Design-aligned requirements

- Connect the five NB-1.0 pages to ten supporting views and complete buyer/provider flows.
- Define 48 refinements and 96 positive/negative scenarios mapped to the existing F/T and DEV IDs; all runtime scenarios remain NOT_EXECUTED.
- Specify hourly GPU pricing, separate input/output M-token rates, exact legacy conversion, cumulative rounding, budget and settlement distinctions.
- Record six unimplemented contract gaps; append CON-047 and update authoritative requirement sources, technical appendices and generated development references.
- Synchronize bilingual documentation and the paused source goal. Product source work and initial repository publication remain paused; no new device, payment or deployment execution.

## R002 — 2026-10-30 — Repository, bilingual docs and real media

- Initialize the Token Share Mesh Git repository and configure the requested GitHub SSH remote.
- Organize source/specifications/evidence, exclude runtime data and preserve exact content-addressed file bytes.
- Add complete English/Chinese READMEs, system/usage/media guides, contribution and ongoing synchronization rules.
- Add reproducible document dependency bootstrap, a restricted loopback reader preview and real browser screenshot/screencast tools.
- Include actual specification-reader screenshots, an MP4 and GIF with source hashes and cleanup/interaction evidence.
- Validate public documentation, links, media, secret exclusions and native-source evidence before publication; keep all 56 implementation tasks visible.
- Record initial publication verification separately from the still-active complete-platform development goal.

## R001 — 2026-10-30 — Native backend foundation

- Add a single Go module, PostgreSQL17 embedded migrations for 60 domain tables and checksum drift rejection.
- Implement bounded integer/rational money, scoped API-key service operations and transactional idempotency.
- Add inbox deduplication and fenced durable jobs with bounded query/cleanup/notification retries.
- Pass 10 actual native foundation tests, Go dependency/format/vet checks, and Windows/Linux admin CLI builds; Linux is cross-compiled only.
- Register every task, numbered substep and pending acceptance gate. Full login, marketplace UI, provider engines, encryption and payment integrations remain incomplete.

