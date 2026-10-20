# EByte

## Project facts

Domain: ebyte.org, to be connected later.
Code: https://github.com/softdevstudio/ebyte, main only.
Website repository: not created; user will supply it later.
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

October 20, 2026: Sessions one through six are complete and session seven is in final release verification. YK selected Apache License 2.0 and authorized proceeding. LICENSE/NOTICE and the manual reflect that choice, an external-package Go example demonstrates the supported API, and the first implemented release version is 0.1.0. No placeholder release or retry-only version bump was used.

Automatic Release follows successful main-push CI for the same current SHA. Linux, macOS and Windows packaging jobs run the library tests, build five version-free archives and smoke-test their native CLI before and after archiving. Cross-built targets are explicitly labelled unexecuted. Archives include examples, project licensing, exact-toolchain GO-NOTICES with hashes, and source/build metadata. The publishing gate checks all archive hashes, clean exact-source Go build metadata, notices and three native results before creating a draft; upload failures clean owned drafts/tags, with failures reported explicitly.

The workflow verifies every public asset at both its version tag and latest, then runs a downloaded native binary. Test fixtures cover archive traversal/link rejection, cross-host credential stripping and failed draft cleanup/public verification paths. Windows deliberately retains stdout-only file output until native no-replace publication support is verified. Five platform archives are not a claim of identical OS features or native execution on every CPU architecture.

Independent release/API/security review without implementation history found no blockers in source gates, packaging, native/cross labels, licensing, draft cleanup or public verification. Local Go tests, the external-package example and native Linux archive smoke passed; the final recorder reruns formatting, Python safety, vet/race, all six fuzz targets, real process smoke, demo and resource observations. Confirm final Linux/macOS CI, all native package jobs and public tag/latest verification before marking session seven complete; save public verification in test/results/ebyte-0.1.0-release and update this handoff afterward.

No website/domain or studio work has been done. The site repository and domain connection are still supplied later by YK. Blocking I/O, materialized-memory and platform file-publication limits remain documented.

## Next

Finish and verify the automated 0.1.0 release, record actual source SHA/native labels/public hashes/downloaded-binary checks and report any failed draft/tag in the same session. Do not bump a version merely to retry a gate before publication. Once verified, pause and report. Session eight starts only after YK supplies the website repository and ebyte.org connection; preserve its supplied Hugo template/major structure and replace content/config only. Follow the website/studio standards, read the studio CONTENT-PLAN.md before the launch listing/note, and finish live-site checks. Do not invent or create a website repository early.
