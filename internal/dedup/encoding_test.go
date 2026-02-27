package dedup

import (
	"math"
	"testing"
)

func TestEncodeDecodeRoundtrip(t *testing.T) {
	tests := []struct {
		name string
		vec  []float32
	}{
		{"simple", []float32{1.0, 2.0, 3.0}},
		{"negative", []float32{-1.0, -0.5, 0.0, 0.5, 1.0}},
		{"small values", []float32{0.001, 0.002, 0.003}},
		{"large values", []float32{1e10, -1e10, 3.14159}},
		{"single element", []float32{42.0}},
		{"many elements", make([]float32, 1536)}, // typical embedding size
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := EncodeEmbedding(tt.vec)
			decoded := DecodeEmbedding(encoded)

			if len(decoded) != len(tt.vec) {
				t.Fatalf("length mismatch: expected %d, got %d", len(tt.vec), len(decoded))
			}

			for i := range tt.vec {
				if tt.vec[i] != decoded[i] {
					t.Errorf("value mismatch at index %d: expected %f, got %f", i, tt.vec[i], decoded[i])
				}
			}
		})
	}
}

func TestEncodeDecodeSpecialValues(t *testing.T) {
	vec := []float32{
		0,
		float32(math.Inf(1)),
		float32(math.Inf(-1)),
		float32(math.NaN()),
		math.MaxFloat32,
		math.SmallestNonzeroFloat32,
	}

	encoded := EncodeEmbedding(vec)
	decoded := DecodeEmbedding(encoded)

	if len(decoded) != len(vec) {
		t.Fatalf("length mismatch: expected %d, got %d", len(vec), len(decoded))
	}

	// Check non-NaN values
	if decoded[0] != 0 {
		t.Errorf("expected 0, got %f", decoded[0])
	}
	if !math.IsInf(float64(decoded[1]), 1) {
		t.Errorf("expected +Inf, got %f", decoded[1])
	}
	if !math.IsInf(float64(decoded[2]), -1) {
		t.Errorf("expected -Inf, got %f", decoded[2])
	}
	if !math.IsNaN(float64(decoded[3])) {
		t.Errorf("expected NaN, got %f", decoded[3])
	}
	if decoded[4] != math.MaxFloat32 {
		t.Errorf("expected MaxFloat32, got %f", decoded[4])
	}
	if decoded[5] != math.SmallestNonzeroFloat32 {
		t.Errorf("expected SmallestNonzeroFloat32, got %e", decoded[5])
	}
}

func TestDecodeEmptySlice(t *testing.T) {
	decoded := DecodeEmbedding(nil)
	if decoded != nil {
		t.Errorf("expected nil, got %v", decoded)
	}

	decoded = DecodeEmbedding([]byte{})
	if decoded != nil {
		t.Errorf("expected nil for empty slice, got %v", decoded)
	}
}

func TestEncodeEmptySlice(t *testing.T) {
	encoded := EncodeEmbedding([]float32{})
	if len(encoded) != 0 {
		t.Errorf("expected empty slice, got %d bytes", len(encoded))
	}
}

func TestEncodedSize(t *testing.T) {
	// Each float32 is 4 bytes
	vec := []float32{1.0, 2.0, 3.0, 4.0, 5.0}
	encoded := EncodeEmbedding(vec)
	expectedLen := len(vec) * 4
	if len(encoded) != expectedLen {
		t.Errorf("expected %d bytes, got %d", expectedLen, len(encoded))
	}
}
