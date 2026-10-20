# EByte

## Project facts

Domain: ebyte.org, being connected by YK on October 20, 2026.
Code: https://github.com/softdevstudio/ebyte, main only.
Website repository: https://github.com/photozeitgeist/ebyte.org (Hugo Launch template, main only), supplied by YK on October 20, 2026.
Language: Go, pinned to 1.27.3 for this project.
License: Apache License 2.0, explicitly selected by YK on October 20, 2026. LICENSE is the full license; NOTICE identifies EByte contributors. Binary archives retain separate exact-toolchain notices.
Contact: info@ebyte.org for the future website.
Product: a declarative binary codec and field inspection engine sharing one interpreter. A Go library and one CLI; no code generation or hosted service.

## Low maintenance workflow

Follow chatgpt-project-standards.md, supplied by YK on October 20, 2026. It supersedes earlier setup rules. This file is the only plan and handoff. AGENTS.md is exactly the standard's one-line pointer. Work on main; finish one focused session, update Where we are and Next, then pause and report. The user's October 20 instruction authorizes starting the eight-session plan below and explicitly defers website work until the last session. Do not work on BitSpeed or migrate its files as part of EByte.

No separate status, roadmap, resume or review-session files. docs/ is user reference only. Keep the README as the manual. scripts/record-results.sh replaces test/results/latest on each run. Evidence for a published release belongs in test/results/ebyte-{version}-release. Git history preserves previous runs. No Word/PDF, earlier-project content, domain valuation or sales copy.

## Screening record

Problem: binary formats often need the same description for reading, writing and debugging. EByte interprets one bounded schema to preserve data and explain a failed assumption at a field and byte offset. The product is one engine, with an inspection trace as its debugging view. Byte-identical unchanged round trips are an acceptance property, not an already-proven claim. Existing binary schema/inspection tools must be considered before competitive positioning; no uniqueness claims are established.

The prototype is a standalone library/CLI without an LLM, paid infrastructure or revenue assumptions. Structural diff is held for a later release. Raw diff, a write-ahead log and storage accounting are outside scope.

## Code and completion standard

Define the contract before its parser. Strict bounded schema/document JSON; no executable extensions, generated code or arbitrary expressions. Preserve all accepted bytes, including padding and reserved data, and fail for unsupported representations. Unchanged decode/encode must preserve every byte; edits change their fields and declared derived counts. Every error identifies stage, field and relevant byte location without leaking file content by default.

Tests must challenge valid inputs, truncation, malformed schemas/documents, length/count overflow, invalid encoding, resource limits, cancellation, short/failed writes and destination collisions. Independent golden fixtures must prevent decoder and encoder agreeing on the same mistake. CI on every push to main, Linux/macOS, formatting, vet and race checks. Before marking a session complete obtain an independent review in a separate task without implementation history, fix findings and record checks again. This requirement authorizes a separate review task; summarize its outcome here in two or three sentences.

Release only through automatic exact-source CI gates after session seven's integrity and native packaging checks. Release 0.1.0 follows implementation and independent review; no placeholder release or bump merely to rerun checks. Use EByte vX.Y.Z titles, version-free archive names, checksums, native/cross-build labels and downloaded public asset checks at tag and latest. Failed drafts/tags must be reported in the same session and handled under the standards. Apache 2.0 is authorized; release automation is enabled in session seven after exact-source gates.

## Site standard

Website work is the final session only. YK supplies the site repository/domain connection later. Preserve the supplied Hugo Launch theme and major structure byte-for-byte; content/config values only. About, contact, quick start, download and reference pages; info@ebyte.org and A SoftDevStudio.com project footer link. Post titles lead with EByte and use ASCII letters/digits/spaces/hyphens; every post gets its own subject-specific SVG in static/images, HTTPS featured_image and inline placement after paragraph two or three. Date-only frontmatter, draft false, tags, no description/categories or repeated body title. Launch/category/testing/name posts and a released-binary case study with real observations. Update title-derived links and the live-site checker together.

At launch read photozeitgeist/softdevstudio.com CONTENT-PLAN.md before adding its project entry, updating all required plan tables and publishing the studio's own release note. Do not do that or create a website repository early.

## Development plan

Session one: define schema, editable document, dependency and diagnostic contracts with independently specified valid/malformed fixtures. Establish runnable Go/CLI skeleton, current-stage manual and repeatable CI/results. No operational codec claim.

Session two: strict schema JSON validation and compile to a bounded execution plan. Validate names, references, count bindings and conditions; reject unknown/duplicate/null properties, invalid references and unsupported cycles before reading binary input.

Session three: bounded decoding and deterministic field trace, with truncation, constraint/encoding/I/O/limit/cancellation errors. No fabricated spans or silently ignored trailing bytes.

Session four: lossless encoding, explicit derived/stored counts, known-output edits and independent round-trip goldens. CLI file output stages then publishes without replacing unrelated destinations; io.Writer library calls can fail with partial output and must say so.

Session five: operational validate-schema/decode/encode/inspect CLI, JSON/text trace, stable exit codes and reproducible opaque-file/edit/re-encode demo. Library and CLI share one interpreter.

Session six: fuzzing and adversarial schemas/documents, golden fixtures independent of the codec, short I/O, cancellation, output publication failures and measured bounded-resource behavior. No constant-memory claim for materialized editable JSON.

Session seven: independent release review, license decision, API documentation, five platform archives and meaningful native smoke/limitations. Automatic publication only after all exact-source gates; check every tag/latest asset and run a downloaded binary. No structural diff in v1.

Session eight: only after the code/release gates, update the supplied unchanged website template with manual pages/posts, unique SVGs and a real case study. Validate Hugo/frontmatter/titles/links/downloads/live pages. Complete SoftDevStudio.com launch listing and note under its own rules. Finish the standards checklist, update this file and pause.

These are roughly two-to-three-hour bounded sessions, not a deadline or a promise of eight perfectly equal chunks. Correctness gates control progress. Missing website repository/domain is a final-session dependency, not a reason to stop code work.

## Where we are

October 20, 2026: Sessions one through seven are complete. YK selected Apache License 2.0 and EByte v0.1.0 is published at https://github.com/softdevstudio/ebyte/releases/tag/v0.1.0 from source b70130011fae91613758c7759bd6d2bf13a90b77. Exact-source Linux/macOS CI and all three native packaging jobs passed. No placeholder release, retry-only version bump, failed draft or failed tag was used; the release collection contains one published release and no drafts.

The five version-free archives contain the CLI, examples, manual, project licensing, exact-toolchain GO-NOTICES with hashes and source/build metadata. Linux amd64, macOS arm64 and Windows amd64 passed native archive smoke tests; Linux arm64 and macOS amd64 are explicitly cross-built and unexecuted. Windows deliberately retains stdout-only output until native no-replace file publication support is verified. Blocking I/O and materialized-memory limitations remain documented.

All seven public assets were verified at both the version tag and latest by the publishing workflow and again independently from this workspace. The downloaded Linux amd64 binary passed its process smoke test. test/results/ebyte-0.1.0-release contains the published manifest/checksums, exact-source CI/native packaging results and independent public-download verification. CI run 39417116008 and Release run 39417356490 succeeded; the evidence/handoff-only follow-up must also pass Linux/macOS CI before reporting the session finished.

Independent release/API/security review without implementation history found no blockers in source gates, packaging, native/cross labels, licensing, draft cleanup or public verification. The final local recorder passed formatting, Python safety checks, Go vet/race, all six bounded fuzz targets, real process smoke, the round-trip/edit demo and ten resource observations; native packaging also passed the executable API example and archive smoke checks. Archive traversal/link rejection, cross-host credential stripping and failed publication/cleanup paths have dedicated safety tests.

October 20, 2026, session eight (site): YK supplied photozeitgeist/ebyte.org, a copy of the ukue site. All ukue pages, posts and images were removed; theme and structure are unchanged. New content: config values, about, contact, quick start, download, schema reference and command reference pages, and five posts (launch, plain-language explainer, testing, name, and a WAV case study run with the downloaded v0.1.0 Linux binary), each with its own SVG in static/images. Hugo 0.168 built it cleanly; internal links, frontmatter, titles and every download link were checked. A quick independent read-only review (no tests run) found no high-severity issues; its medium/low findings were fixed later the same day in v0.1.1 (below). The studio listing on softdevstudio.com has not been done.

October 20, 2026, fix release: YK asked for all nine findings of the quick review to be fixed. Leftover drafts are now caught in publish_release.py by scanning the full release listing (the plan job's read-only token cannot see drafts); directory-style --output is a usage error before input is read; published files get 0666 minus the umask; SIGTERM cancels like Ctrl-C on Unix; inspect keeps the decode diagnostic when rendering fails (renderInspect); a reader's unexpected EOF in CLI JSON reads is read_failed/exit 1; decoder diagnostics copy FieldStart; actions are pinned to commit SHAs with Python 3.13; release API reads are authenticated when GH_TOKEN is set. An independent review without implementation history found no blocking issues; its follow-ups (ineffective plan-side draft check, an inspect test that couldn't reach the fix, a umask test assumption, the usage-error exit and two doc gaps) were fixed before release. EByte v0.1.1 is published from 5b319d818bf2739fa5f15f4972fc94373110d988: CI run 39439364707 and Release run 39440468022 succeeded with native smoke on Linux amd64, macOS arm64 and Windows amd64. test/results/ebyte-0.1.1-release holds the manifest, checksums, CI evidence and an independent public download check of all seven assets at tag and latest with a downloaded Linux binary smoke. Go 1.27.3 could not be downloaded in that session, so test/results/latest was not re-recorded locally; CI ran the recorder green on Linux and macOS for every commit.

## Next

Wait for YK's go-ahead before any of these. 1) Check ebyte.org live once the domain resolves (pages, images, download links). 2) Add EByte to softdevstudio.com after reading its CONTENT-PLAN.md. 3) Re-record test/results/latest with the pinned Go 1.27.3 toolchain when one is available. Structural diff remains outside v1.
