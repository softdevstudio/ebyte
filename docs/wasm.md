# WebAssembly build

Since EByte 0.7.0 every release has an ebyte-wasm.zip asset: ebyte.wasm, built from the release commit with the pinned Go toolchain for GOOS=js GOARCH=wasm, the same toolchain's wasm_exec.js, LICENSE, NOTICE, THIRD_PARTY_LICENSES.md, GO-NOTICES and release.json with the hashes of both files. It runs the same library as the CLI, with DefaultLimits, entirely in the page that loads it; nothing is sent anywhere. The ebyte.org demo serves these two files unchanged.

Load wasm_exec.js as a classic script, then instantiate ebyte.wasm with a Go object's importObject and call go.run(instance). That defines a global ebyte object:

- ebyte.version: the release version string.
- ebyte.build: the embedded Go build information: go_version, GOOS, GOARCH, vcs.revision and vcs.modified.
- ebyte.validate(schemaText)
- ebyte.inspect(schemaText, Uint8Array)
- ebyte.decode(schemaText, Uint8Array)
- ebyte.diff(schemaText, Uint8Array before, Uint8Array after)
- ebyte.encode(schemaText, documentText) returns {json, bytes}: bytes is a Uint8Array, or null when encoding failed.

Every call returns (or, for encode, carries in json) one JSON envelope with exactly ok, schema ({name, sha256} once the schema has compiled, otherwise null), result and diagnostic, plus input ("before" or "after") when a diff input failed. On success ok is true, result is what the CLI would print (the validated schema identity, a trace, a document, {"bytes": n} for encode, or a diff result) and diagnostic is null. On failure ok is false and diagnostic is the usual errors-v1 diagnostic; a failed inspect still carries its partial, labelled trace in result, as the CLI's inspect does. A call with the wrong number or kind of arguments (not a string, not a Uint8Array) returns ok false with stage io and code invalid_argument, and an input larger than the 16 MiB input ceiling returns limit_exceeded before it is copied; neither stops the runtime, so later calls still work. Calls are synchronous and run on the page's thread, so a large file blocks the page until it finishes; there is no cancellation from JavaScript. Memory is the browser's, bounded by the same limits as the library.
