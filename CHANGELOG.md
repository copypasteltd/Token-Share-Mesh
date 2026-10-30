# Changelog

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

