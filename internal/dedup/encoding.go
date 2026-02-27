package dedup

import (
	"encoding/binary"
	"math"
)

// EncodeEmbedding serializes a float32 slice to a binary BLOB using little-endian encoding.
func EncodeEmbedding(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// DecodeEmbedding deserializes a binary BLOB back to a float32 slice using little-endian encoding.
func DecodeEmbedding(b []byte) []float32 {
	if len(b) == 0 {
		return nil
	}

	n := len(b) / 4
	v := make([]float32, n)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}
