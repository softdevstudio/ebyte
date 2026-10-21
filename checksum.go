package ebyte

import (
	"hash/adler32"
	"hash/crc32"
)

// checksum is a running checksum over bytes as they are read or written.
type checksum interface {
	Write(p []byte) (int, error)
	Sum32() uint32
}

// checksumWidths lists every supported algorithm with the integer type its
// value must be stored in.
var checksumWidths = map[string]string{
	"crc32":        "u32",
	"adler32":      "u32",
	"crc16_arc":    "u16",
	"crc16_modbus": "u16",
	"crc16_xmodem": "u16",
}

func newChecksum(algorithm string) checksum {
	switch algorithm {
	case "crc32":
		return crc32.NewIEEE()
	case "adler32":
		return adler32.New()
	case "crc16_arc":
		return &crc16{reflected: true}
	case "crc16_modbus":
		return &crc16{reflected: true, value: 0xffff}
	default:
		return &crc16{}
	}
}

// crc16 computes the reflected 0x8005 variants (ARC, Modbus) or the
// non-reflected 0x1021 variant (XMODEM) bit by bit; no final XOR.
type crc16 struct {
	reflected bool
	value     uint16
}

func (c *crc16) Write(p []byte) (int, error) {
	for _, b := range p {
		if c.reflected {
			c.value ^= uint16(b)
			for i := 0; i < 8; i++ {
				if c.value&1 != 0 {
					c.value = c.value>>1 ^ 0xa001
				} else {
					c.value >>= 1
				}
			}
			continue
		}
		c.value ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if c.value&0x8000 != 0 {
				c.value = c.value<<1 ^ 0x1021
			} else {
				c.value <<= 1
			}
		}
	}
	return len(p), nil
}
func (c *crc16) Sum32() uint32 { return uint32(c.value) }
