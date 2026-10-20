// Command resourcecheck is an internal single-sample evidence runner, not a benchmark.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/softdevstudio/ebyte"
)

type countedReader struct {
	*bytes.Reader
	read, calls int
}

func (r *countedReader) Read(p []byte) (int, error) {
	r.calls++
	n, e := r.Reader.Read(p)
	r.read += n
	return n, e
}

type observation struct {
	Case               string       `json:"case"`
	GoVersion          string       `json:"go_version"`
	GOOS               string       `json:"goos"`
	GOARCH             string       `json:"goarch"`
	Samples            int          `json:"samples"`
	SchemaSHA256       string       `json:"schema_sha256"`
	InputBytes         int          `json:"input_bytes"`
	ReadBytes          int          `json:"read_bytes"`
	ReadCalls          int          `json:"read_calls"`
	DocumentBytes      int          `json:"document_bytes"`
	TraceBytes         int          `json:"trace_bytes"`
	TraceEntries       int          `json:"trace_entries"`
	OutputBytes        int          `json:"output_bytes"`
	InputSHA256        string       `json:"input_sha256"`
	OutputSHA256       string       `json:"output_sha256"`
	RoundTrip          bool         `json:"round_trip_identical"`
	Diagnostic         string       `json:"diagnostic_code"`
	ElapsedNanoseconds int64        `json:"elapsed_nanoseconds"`
	AllocatedBytes     uint64       `json:"total_allocated_bytes"`
	Allocations        uint64       `json:"allocations"`
	CPUs               int          `json:"logical_cpus"`
	MaxProcs           int          `json:"gomaxprocs"`
	HeapAllocBefore    uint64       `json:"heap_alloc_before_bytes"`
	HeapSysBefore      uint64       `json:"heap_sys_before_bytes"`
	HeapAllocAfter     uint64       `json:"heap_alloc_after_bytes"`
	HeapSysAfter       uint64       `json:"heap_sys_after_bytes"`
	Collections        uint32       `json:"gc_cycles"`
	Limits             ebyte.Limits `json:"limits"`
}

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("one internal case name is required")
	}
	name := os.Args[1]
	limits := ebyte.DefaultLimits()
	var fields string
	var input []byte
	reject := false
	sizes := map[string]int{"bytes-0": 0, "bytes-64KiB": 64 << 10, "bytes-1MiB": 1 << 20, "bytes-8MiB": 8 << 20, "bytes-16MiB": 16 << 20}
	if size, ok := sizes[name]; ok {
		fields = `[{"name":"body","type":"bytes","length":` + strconv.Itoa(size) + `}]`
		input = make([]byte, size)
		for i := range input {
			input[i] = byte(i)
		}
	} else {
		switch name {
		case "ascii-escape-1MiB":
			input = make([]byte, 1<<20)
			fields = `[{"name":"body","type":"string","length":1048576,"encoding":"ascii"}]`
		case "empty-elements":
			fields = `[{"name":"items","type":"array","count":65536,"fields":[]}]`
		case "node-limit":
			fields = `[{"name":"items","type":"array","count":65536,"fields":[{"name":"empty","type":"bytes","length":0}]}]`
			reject = true
		case "trace-limit":
			fields = `[{"name":"items","type":"array","count":65536,"fields":[]}]`
			limits.MaxTraceEntries = 32
			reject = true
		case "huge-field-limit":
			fields = `[{"name":"n","type":"u64"},{"name":"body","type":"bytes","length":{"field":"n"}}]`
			input = make([]byte, 8)
			binary.LittleEndian.PutUint64(input, ^uint64(0))
			reject = true
		default:
			return fmt.Errorf("unknown internal case")
		}
	}
	schema := []byte(`{"schema_version":1,"name":"resource","endian":"little","fields":` + fields + `}`)
	plan, e := ebyte.Compile(schema, limits)
	if e != nil {
		return e
	}
	result := observation{Case: name, GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Samples: 1, SchemaSHA256: plan.SchemaSHA256(), InputBytes: len(input), Limits: limits}
	reader := &countedReader{Reader: bytes.NewReader(input)}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	started := time.Now()
	document, trace, e := plan.Decode(ctx, reader)
	var encoded bytes.Buffer
	if reject {
		d, ok := e.(*ebyte.Diagnostic)
		if !ok || d.Code != "limit_exceeded" || document != nil {
			return fmt.Errorf("expected bounded rejection, got %v", e)
		}
		result.Diagnostic = d.Code
		if name == "huge-field-limit" && reader.read != 8 {
			return fmt.Errorf("rejected dimension read payload")
		}
	} else {
		if e != nil {
			return e
		}
		documentJSON, e := json.Marshal(document)
		if e != nil {
			return e
		}
		result.DocumentBytes = len(documentJSON)
		loaded, e := plan.LoadDocument(ctx, documentJSON)
		if e != nil {
			return e
		}
		if e = plan.Encode(ctx, loaded, &encoded); e != nil {
			return e
		}
		if !bytes.Equal(input, encoded.Bytes()) {
			return fmt.Errorf("round trip differs")
		}
		result.RoundTrip = true
		result.OutputBytes = encoded.Len()
	}
	if trace != nil {
		serialized, e := json.Marshal(trace)
		if e != nil {
			return e
		}
		result.TraceBytes = len(serialized)
		result.TraceEntries = len(trace.Entries)
		if len(serialized) > limits.MaxTraceBytes || len(trace.Entries) > limits.MaxTraceEntries {
			return fmt.Errorf("trace exceeds ceilings")
		}
	}
	result.ElapsedNanoseconds = time.Since(started).Nanoseconds()
	runtime.ReadMemStats(&after)
	result.ReadBytes = reader.read
	result.ReadCalls = reader.calls
	result.AllocatedBytes = after.TotalAlloc - before.TotalAlloc
	result.Allocations = after.Mallocs - before.Mallocs
	result.CPUs = runtime.NumCPU()
	result.MaxProcs = runtime.GOMAXPROCS(0)
	result.HeapAllocBefore = before.HeapAlloc
	result.HeapSysBefore = before.HeapSys
	result.HeapAllocAfter = after.HeapAlloc
	result.HeapSysAfter = after.HeapSys
	result.Collections = after.NumGC - before.NumGC
	inputSum, outputSum := sha256.Sum256(input), sha256.Sum256(encoded.Bytes())
	result.InputSHA256 = hex.EncodeToString(inputSum[:])
	if !reject {
		result.OutputSHA256 = hex.EncodeToString(outputSum[:])
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
