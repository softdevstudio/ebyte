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

October 20, 2026: Sessions one through five are complete. The built CLI now provides validate-schema, decode, encode and inspect using the shared library, explicit schema/input options, stdin/stdout, JSON/text traces and lowered resource ceilings. Schema validation precedes binary/document input opening. Success results are prepared before output; decode/encode validation failure emits no success bytes or file, while inspect can publish a labelled failed trace with nonzero exit. All file output uses staged no-replace publication. Static usage errors do not echo arguments; operational stderr diagnostics are JSON and use the specified 0/1/2/3/4 exits.

CLI tests compare all four document/byte goldens and the independent packet trace, exercise file/stdin round trips, failed inspect output, exact bounded reads, limits, malformed arguments, schema-before-input behavior, cancellation, writer failures and existing destinations. Compiled-binary smoke verifies real broken pipes and SIGINT with stdin deliberately held open, in addition to command behavior and collision/privacy checks. The real demo starts from the independent 21-byte packet, confirms an unchanged byte-identical round trip, adds the pre-specified record and observes 25 bytes, derived count 3 and note offset 23 rather than 19. Actual platform, hashes, documents and traces are saved to test/results/latest/demo.json; this synthetic example is not a benchmark or format-discovery claim.

Independent review without implementation history found blocked stdin swallowing interrupts and default SIGPIPE bypassing the promised write-failure exit. Interruptible owned stdin buffers, prompt restoration of default interrupt handling and Unix SIGPIPE handling fixed both; compiled subprocess regressions were added. Re-review also caught cancellation replacing a partial-output warning, which was fixed and tested; final review, race tests and vet found no blockers, and the recorder reruns formatting, recorder safety, codec/CLI/publication checks, bounded fuzz, compiled smoke and the saved demo.

The version remains 0.0.0-dev with no public release. CLI stdin cancellation can leave one blocked read worker until process exit; other blocking filesystem calls may require a further interrupt, and library cancellation cannot interrupt arbitrary blocked I/O. No website/domain work or studio listing was published; license selection remains pending before release. Verify Linux/macOS main CI for the final session-five commit before reporting completion.

## Next

After verifying session-five main CI, pause and report. On YK's next go-ahead, start session six: adversarial schema/document/CLI coverage, broader lossless fuzzing across supported shapes, short/failed I/O and cancellation boundaries, and fault-injected publication sync/link/cleanup failures. Measure bounded-resource behavior with reproducible fixtures and saved observations, including materialized JSON/encoded buffers; do not claim constant memory or general throughput from synthetic examples. Check process-level behavior and platform limitations before session seven's native/release gates. Require independent review and recorded results, with no release/version bump in session six. License selection is required before session seven's first public release; website remains session eight after YK supplies its repository/domain connection.
