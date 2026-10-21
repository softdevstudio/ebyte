// Loads ebyte.wasm with the toolchain's wasm_exec.js, the way a browser page
// does, and exercises every exported call on the packet fixture.
// Usage: node scripts/wasm_smoke.mjs DIR VERSION   (DIR holds both files)
import { readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import assert from 'node:assert/strict';

const [dir, version] = process.argv.slice(2);
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
const call = (raw) => JSON.parse(raw);

const valid = call(ebyte.validate(schema));
assert.ok(valid.ok && valid.schema.name === 'ebyte_packet');
assert.equal(call(ebyte.validate('{')).diagnostic.code, 'invalid_schema');

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

console.log(JSON.stringify({ wasm_smoke: 'passed', version }));
process.exit(0);
