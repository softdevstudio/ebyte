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

October 20, 2026: Sessions one through four are complete. The library now strictly loads editable document JSON and encodes validated values through the compiled plan. Derived counts are calculated from bound arrays and omitted from supplied values; stored counts must match. Schema identity, canonical integers/hex, assertions, field widths, ASCII/UTF-8, active/absent fields and nested object shapes are checked. Encoding prepares bounded binary bytes before its first write, so all validation errors write nothing. Writer failures and cancellation during/after Write may leave output and explicitly say so. LoadDocument uses the same preparation for validation; both calls materialize bounded data.

All four independent golden documents encode to the specified bytes and decode/encode round-trip unchanged. All twelve original acceptance vectors now execute against their real compile/decode/encode operations in addition to independent byte oracles. Tests cover the known derived-count edit, count overflow/mismatch, nested dynamic dimensions, strict malformed/duplicate/null JSON and Unicode, inactive/derived values, exact node/document/output ceilings and escape expansion, aggregate output limits, traversal cancellation, short/failed/invalid writer counts and precise assertion/encoding positions. The recorder includes bounded decoder, document-loading/encoding and lossless packet fuzz runs, and continues replacing only test/results/latest.

An internal CLI publication helper stages a private file in an opened parent directory, syncs/closes it, and creates a new hard link without replacement. Native Linux/macOS tests cover files, symlinks, directories, a concurrent destination, producer/sync failures, cancellation, cleanup and renamed parent anchoring. Unsupported platforms/filesystems fail closed; hostile concurrent mutation of entries in the opened parent and power-loss directory durability are outside its guarantee. The helper is not exposed as an operational command yet; fault-injected link/cleanup failures and broader resource measurements remain session six.

Independent review without implementation history found missing known output positions on assertion/ASCII failures and fabricated zero offsets for invalid writer counts. Both were corrected, precise nonzero-prefix regressions and additional escape/cancellation/aggregate-limit tests were added, and re-review plus vet/race checks found no blockers. Final formatting, recorder safety, compiler/codec/publication tests, bounded fuzz, command build and CLI smoke are recorded in the final results run.

The version remains 0.0.0-dev and the CLI supports only help/version. No release, website/domain work or studio listing was published; license selection remains pending before release. Verify Linux/macOS main CI for the final session-four commit before reporting completion.

## Next

After verifying session-four main CI, pause and report. On YK's next go-ahead, start session five: operational validate-schema/decode/encode/inspect commands sharing the library plan, stable diagnostic exit codes, bounded schema/document reads, JSON/text trace and explicit input/output options. Wire file outputs through the staged no-replace helper; buffer bounded success JSON so stdout never receives a partial editable result followed by a decode failure. Report stdout writer failures and clearly label failed inspect traces with nonzero exit. Add an executable opaque-file/decode/edit/re-encode demo with real saved output, CLI tests and smoke checks. Require independent review and recorded results. License selection is required before session seven's first release; website remains session eight after YK supplies its repository/domain connection.
