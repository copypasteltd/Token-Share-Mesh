# Changelog

[简体中文](CHANGELOG.zh-CN.md)

Entries use the same round IDs as the implementation tracker. No entry denotes a
general-availability platform release unless the full acceptance evidence says so.

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

