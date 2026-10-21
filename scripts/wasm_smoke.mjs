// Loads ebyte.wasm with the toolchain's wasm_exec.js, the way a browser page
// does, and exercises every exported call on the packet fixture.
// Usage: node scripts/wasm_smoke.mjs DIR VERSION [REVISION GO_VERSION]
// DIR holds both files. With REVISION and GO_VERSION the embedded build
// information must match them and show an unmodified checkout.
import { readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import assert from 'node:assert/strict';

const [dir, version, revision, goVersion] = process.argv.slice(2);
const root = join(dirname(fileURLToPath(import.meta.url)), '..');
// wasm_exec.js defines globalThis.Go; it is a classic script, so evaluate it.
new Function(readFileSync(join(dir, 'wasm_exec.js'), 'utf8'))();
const go = new globalThis.Go();
const { instance } = await WebAssembly.instantiate(readFileSync(join(dir, 'ebyte.wasm')), go.importObject);
go.run(instance);
const ebyte = globalThis.ebyte;
assert.equal(ebyte.version, version);

const schema = readFileSync(join(root, 'testdata/packet.schema.json'), 'utf8');
const packet = Uint8Array.from(Buffer.from(readFileSync(join(root, 'testdata/packet.hex'), 'utf8').trim(), 'hex'));
const keys = ['diagnostic', 'ok', 'result', 'schema'];
const call = (raw) => {
  const e = JSON.parse(raw);
  const got = Object.keys(e).sort();
  assert.deepEqual(got.filter((k) => k !== 'input'), keys, 'envelope members');
  assert.equal(e.ok, e.diagnostic === null, 'ok exactly when there is no diagnostic');
  return e;
};
if (revision) {
  assert.equal(ebyte.build['vcs.revision'], revision);
  assert.equal(ebyte.build['vcs.modified'], 'false');
  assert.equal(ebyte.build.go_version, goVersion);
}
assert.equal(ebyte.build.GOOS, 'js');
assert.equal(ebyte.build.GOARCH, 'wasm');

const valid = call(ebyte.validate(schema));
assert.ok(valid.ok && valid.schema.name === 'ebyte_packet');
const badSchema = call(ebyte.validate('{'));
assert.ok(badSchema.diagnostic.code === 'invalid_schema' && badSchema.schema === null && badSchema.result === null);

const trace = call(ebyte.inspect(schema, packet));
assert.ok(trace.ok && trace.result.state === 'complete');
const failedTrace = call(ebyte.inspect(schema, packet.slice(0, 16)));
assert.ok(!failedTrace.ok && failedTrace.result.state === 'failed' && failedTrace.diagnostic.code === 'truncated_input');

const decoded = call(ebyte.decode(schema, packet));
assert.ok(decoded.ok);
const encoded = ebyte.encode(schema, JSON.stringify(decoded.result));
assert.ok(call(encoded.json).ok);
assert.deepEqual(Buffer.from(encoded.bytes), Buffer.from(packet));
const refused = ebyte.encode(schema, '{}');
assert.ok(!call(refused.json).ok && refused.bytes === null);

assert.equal(call(ebyte.diff(schema, packet, packet)).result.state, 'same');
const changed = packet.slice(); changed[17] = 0x2d;
const diff = call(ebyte.diff(schema, packet, changed));
assert.ok(diff.ok && diff.result.changes.length === 1 && diff.result.changes[0].path === '/records/1/temperature');
const broken = call(ebyte.diff(schema, packet, packet.slice(0, 16)));
assert.ok(!broken.ok && broken.input === 'after' && broken.diagnostic.code === 'truncated_input');

// Bad calls get an envelope and leave the runtime working.
assert.equal(call(ebyte.validate()).diagnostic.code, 'invalid_argument');
assert.equal(call(ebyte.inspect(schema, new ArrayBuffer(4))).diagnostic.code, 'invalid_argument');
assert.equal(call(ebyte.decode(schema, [1, 2, 3])).diagnostic.code, 'invalid_argument');
assert.equal(call(ebyte.diff(schema, packet)).diagnostic.code, 'invalid_argument');
const misuse = ebyte.encode(schema);
assert.ok(call(misuse.json).diagnostic.code === 'invalid_argument' && misuse.bytes === null);
assert.ok(call(ebyte.decode(schema, packet)).ok, 'runtime still alive after bad calls');
assert.equal(call(ebyte.diff(schema, packet, packet.slice(0, 16))).input, 'after');

console.log(JSON.stringify({ wasm_smoke: 'passed', version, build: ebyte.build }));
process.exit(0);
