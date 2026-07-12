// Package vectorstore provides utility functions for vector operations
// used by the memory, kb, and experience modules.
package vectorstore

import (
	"math"

	"github.com/viterin/vek/vek32"
)

// CosineSimilarity computes cosine similarity between two float32 vectors.
func CosineSimilarity(a, b []float32) float64 {
	return float64(vek32.CosineSimilarity(a, b))
}

// Float32SliceToBlob converts a float32 slice to a binary blob for SQLite storage.
// Each float32 is encoded as 4 bytes using IEEE 754 binary representation
// in little-endian byte order.
//
// This encoding is efficient (no serialization overhead) and portable
// (IEEE 754 is a universal standard for floating-point numbers).
func Float32SliceToBlob(slice []float32) []byte {
	buf := make([]byte, len(slice)*4)

	for i, v := range slice {
		bits := math.Float32bits(v)
		buf[i*4] = byte(bits)
		buf[i*4+1] = byte(bits >> 8)
		buf[i*4+2] = byte(bits >> 16)
		buf[i*4+3] = byte(bits >> 24)
	}

	return buf
}

// BlobToFloat32Slice converts a binary blob back to a float32 slice.
// This is the inverse operation of Float32SliceToBlob.
// Returns nil if the blob length is not a multiple of 4.
func BlobToFloat32Slice(blob []byte) []float32 {
	if len(blob)%4 != 0 {
		return nil
	}

	slice := make([]float32, len(blob)/4)

	for i := range slice {
		bits := uint32(blob[i*4]) |
			uint32(blob[i*4+1])<<8 |
			uint32(blob[i*4+2])<<16 |
			uint32(blob[i*4+3])<<24
		slice[i] = math.Float32frombits(bits)
	}

	return slice
}
