//go:build js && wasm

// Command ebyte-wasm exposes EByte to JavaScript as a global "ebyte" object.
// Build with GOOS=js GOARCH=wasm and load with the toolchain's wasm_exec.js.
// See docs/wasm.md.
package main

import (
	"context"
	"fmt"
	"runtime/debug"
	"syscall/js"

	"github.com/softdevstudio/ebyte"
	"github.com/softdevstudio/ebyte/internal/web"
)

// args checks a call's arguments: "s" for a string, "b" for a Uint8Array no
// larger than the input ceiling. It returns a usage envelope when they don't fit.
func args(a []js.Value, want string) string {
	if len(a) != len(want) {
		return web.Usage(fmt.Sprintf("%d arguments", len(want)))
	}
	uint8Array := js.Global().Get("Uint8Array")
	for i, kind := range want {
		switch kind {
		case 's':
			if a[i].Type() != js.TypeString {
				return web.Usage("a string argument")
			}
		case 'b':
			if a[i].Type() != js.TypeObject || !a[i].InstanceOf(uint8Array) {
				return web.Usage("a Uint8Array argument")
			}
			if a[i].Get("length").Int() > ebyte.DefaultLimits().MaxInputBytes {
				return web.TooLarge()
			}
		}
	}
	return ""
}

func bytesArg(v js.Value) []byte {
	b := make([]byte, v.Get("length").Int())
	js.CopyBytesToGo(b, v)
	return b
}

// call wraps a JavaScript entry point so a bad call returns an envelope and
// a panic never stops the Go runtime the page depends on.
func call(want string, fn func([]js.Value) any) js.Func {
	return js.FuncOf(func(_ js.Value, a []js.Value) (result any) {
		defer func() {
			if recover() != nil {
				result = web.Usage("arguments EByte can read")
			}
		}()
		if usage := args(a, want); usage != "" {
			return usage
		}
		return fn(a)
	})
}

func build() js.Value {
	info := js.Global().Get("Object").New()
	if bi, ok := debug.ReadBuildInfo(); ok {
		info.Set("go_version", bi.GoVersion)
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision", "vcs.modified", "GOOS", "GOARCH":
				info.Set(s.Key, s.Value)
			}
		}
	}
	return info
}

func main() {
	ctx := context.Background()
	api := js.Global().Get("Object").New()
	api.Set("version", ebyte.Version)
	api.Set("build", build())
	api.Set("validate", call("s", func(a []js.Value) any { return web.Validate(a[0].String()) }))
	api.Set("inspect", call("sb", func(a []js.Value) any { return web.Inspect(ctx, a[0].String(), bytesArg(a[1])) }))
	api.Set("decode", call("sb", func(a []js.Value) any { return web.Decode(ctx, a[0].String(), bytesArg(a[1])) }))
	api.Set("diff", call("sbb", func(a []js.Value) any {
		return web.Diff(ctx, a[0].String(), bytesArg(a[1]), bytesArg(a[2]))
	}))
	encode := call("ss", func(a []js.Value) any {
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
	})
	// A usage failure from call is a plain string; give encode its usual shape.
	api.Set("encode", js.FuncOf(func(this js.Value, a []js.Value) any {
		passed := make([]any, len(a))
		for i, v := range a {
			passed[i] = v
		}
		r := encode.Invoke(passed...)
		if r.Type() == js.TypeString {
			result := js.Global().Get("Object").New()
			result.Set("json", r)
			result.Set("bytes", js.Null())
			return result
		}
		return r
	}))
	js.Global().Set("ebyte", api)
	select {}
}
