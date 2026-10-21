//go:build js && wasm

// Command ebyte-wasm exposes EByte to JavaScript as a global "ebyte" object.
// Build with GOOS=js GOARCH=wasm and load with the toolchain's wasm_exec.js.
// See docs/wasm.md.
package main

import (
	"context"
	"syscall/js"

	"github.com/softdevstudio/ebyte"
	"github.com/softdevstudio/ebyte/internal/web"
)

func bytesArg(v js.Value) []byte {
	b := make([]byte, v.Get("length").Int())
	js.CopyBytesToGo(b, v)
	return b
}

func main() {
	ctx := context.Background()
	api := js.Global().Get("Object").New()
	api.Set("version", ebyte.Version)
	api.Set("validate", js.FuncOf(func(_ js.Value, a []js.Value) any { return web.Validate(a[0].String()) }))
	api.Set("inspect", js.FuncOf(func(_ js.Value, a []js.Value) any { return web.Inspect(ctx, a[0].String(), bytesArg(a[1])) }))
	api.Set("decode", js.FuncOf(func(_ js.Value, a []js.Value) any { return web.Decode(ctx, a[0].String(), bytesArg(a[1])) }))
	api.Set("diff", js.FuncOf(func(_ js.Value, a []js.Value) any {
		return web.Diff(ctx, a[0].String(), bytesArg(a[1]), bytesArg(a[2]))
	}))
	api.Set("encode", js.FuncOf(func(_ js.Value, a []js.Value) any {
		out, envelope := web.Encode(ctx, a[0].String(), a[1].String())
		result := js.Global().Get("Object").New()
		result.Set("json", envelope)
		if out != nil {
			array := js.Global().Get("Uint8Array").New(len(out))
			js.CopyBytesToJS(array, out)
			result.Set("bytes", array)
		} else {
			result.Set("bytes", js.Null())
		}
		return result
	}))
	js.Global().Set("ebyte", api)
	select {}
}
