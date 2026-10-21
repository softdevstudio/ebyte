package ebyte

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var floatDecimal = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// formatFloat renders IEEE 754 bits of a 4- or 8-byte float in the document
// form: shortest round-trip decimal, "inf"/"-inf", or "nan:" and every bit.
func formatFloat(bits uint64, size int) string {
	if size == 4 {
		v := math.Float32frombits(uint32(bits))
		switch {
		case v != v:
			return fmt.Sprintf("nan:%08x", uint32(bits))
		case math.IsInf(float64(v), 1):
			return "inf"
		case math.IsInf(float64(v), -1):
			return "-inf"
		}
		return strconv.FormatFloat(float64(v), 'g', -1, 32)
	}
	v := math.Float64frombits(bits)
	switch {
	case math.IsNaN(v):
		return fmt.Sprintf("nan:%016x", bits)
	case math.IsInf(v, 1):
		return "inf"
	case math.IsInf(v, -1):
		return "-inf"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// parseFloat converts a document value to the bits of a 4- or 8-byte float.
// The code is "" on success, otherwise invalid_document or value_out_of_range.
func parseFloat(s string, size int) (uint64, string) {
	switch {
	case s == "inf":
		if size == 4 {
			return uint64(math.Float32bits(float32(math.Inf(1)))), ""
		}
		return math.Float64bits(math.Inf(1)), ""
	case s == "-inf":
		if size == 4 {
			return uint64(math.Float32bits(float32(math.Inf(-1)))), ""
		}
		return math.Float64bits(math.Inf(-1)), ""
	case strings.HasPrefix(s, "nan:"):
		digits := s[4:]
		if len(digits) != 2*size || strings.ToLower(digits) != digits {
			return 0, "invalid_document"
		}
		bits, e := strconv.ParseUint(digits, 16, 8*size)
		if e != nil || formatFloat(bits, size) != s {
			// Not hex, or the pattern is not a NaN.
			return 0, "invalid_document"
		}
		return bits, ""
	case !floatDecimal.MatchString(s):
		return 0, "invalid_document"
	}
	v, e := strconv.ParseFloat(s, 8*size)
	if e != nil && (math.IsInf(v, 0) || !strings.Contains(e.Error(), "range")) {
		return 0, "value_out_of_range"
	}
	if size == 4 {
		return uint64(math.Float32bits(float32(v))), ""
	}
	return math.Float64bits(v), ""
}
