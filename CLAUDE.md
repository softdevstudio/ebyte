# EByte

## Project facts

Domain: ebyte.org, to be connected later.
Code: https://github.com/softdevstudio/ebyte, main only.
Website repository: not created; user will supply it later.
Language: Go, pinned to 1.27.3 for this project.
License: undecided. No public release until YK selects the license; no assumption that another project's license carries over. LICENSE and NOTICE currently explain the pending decision.
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

Release only through automatic exact-source CI gates after session seven's integrity and native packaging checks. Remain 0.0.0-dev until implemented/reviewed; no placeholder release or bump merely to rerun checks. Use EByte vX.Y.Z titles, version-free archive names, checksums, native/cross-build labels and downloaded public asset checks at tag and latest. Failed drafts/tags must be reported in the same session and handled under the standards. Select the license before a public release; no release automation is enabled in session one.

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

October 20, 2026: Sessions one through six are complete. Hardening adds adversarial UTF-8/escaping checks, repeated derived-count accounting, huge dimension rejection before payload reads and broad lossless fuzzing across integer widths/signs/byte orders, nested/stored/derived arrays, dynamic lengths, conditionals, strings and assertions. Integer fuzz observations also use an independent encoding/binary oracle. Six bounded fuzz targets now include schema parsing and CLI arguments alongside packet/document/shape cases; existing independent fixtures and process-level smoke remain in place.

Publication fault injection covers create, sync, close, link, cleanup before and after linking and cancellation after sync. It exposed a previously silent pre-publication cleanup failure, which is now reported even when an earlier operation failed. Post-link cleanup failure preserves the complete destination and warns that it and a staging file may remain. Cancellation retains those warnings and known field/byte positions rather than replacing them with generic text. No global test hooks, new dependencies or release features were introduced.

Ten fresh-process resource cases cover deterministic bytes through exactly 16 MiB, 1 MiB of escape-heavy ASCII, 65536 empty objects, the node ceiling, a lowered trace ceiling and rejection of the maximum u64 field length after only eight input bytes. Actual observations are regenerated into test/results/latest/resources.json, with the existing demo and checks log in that same folder. The measured pipeline materializes documents, JSON, traces and output; cumulative allocations can greatly exceed input size. Before/after Go heap snapshots and allocation deltas are explicitly distinguished from peak RSS, hard memory guarantees and throughput benchmarks. docs/testing.md documents the harness and its limits.

Independent review without implementation history found no correctness, security, contract or measurement blockers. Re-review checked the final cancellation preservation, integer oracle and measurement metadata changes; race tests, vet, the resource cases and shape fuzz passed. The final recorder reruns formatting, recorder safety, codec/CLI/publication tests, all six fuzz targets, compiled process smoke, the real demo and ten isolated resource observations.

The version remains 0.0.0-dev and no release/tag or website/studio changes were made. File publication remains enabled only on native-tested Linux/macOS, without hostile-parent-entry or power-loss durability guarantees; blocking filesystem I/O retains the documented cancellation limitations. License selection remains pending and must precede publication. Verify Linux/macOS main CI for the final session-six commit before reporting completion.

## Next

After verifying session-six main CI, pause and report. On YK's next go-ahead, start session seven: final independent release/API review, settle the license before any publication, build five platform archives with version-free names and explicit native/cross-build labels, add meaningful native smoke and limitations, and establish exact-source automatic release gates. Prepare and verify all authorized engineering work before requesting any remaining license decision; do not publish a placeholder or guess another project's terms. Public releases must use EByte vX.Y.Z titles and verified tag/latest assets, checksums and a downloaded binary. Do not bump merely to retry. Website remains session eight after the code/release gates and after YK supplies its repository/domain connection.
