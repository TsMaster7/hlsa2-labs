// Package payload generates deterministic payloads of two sizes for
// the lookup operation. step 3's bench needs the same payload across
// REST and gRPC so the only delta is encoding + transport.
package payload

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

const (
	// SizeSmall is ~256 bytes - the boring, low-volume regime.
	SizeSmall = "small"
	// SizeLarge is ~4 KB nested - the chatty regime where gRPC's
	// compact binary encoding starts to win on the wire and on CPU.
	SizeLarge = "large"
)

// Bytes returns deterministic bytes for the given key/size combination.
// "Deterministic" means a given (key, size) pair always yields the same
// payload so the bench results are stable across runs.
func Bytes(key, size string) []byte {
	switch size {
	case SizeLarge:
		return makeLarge(key)
	default:
		return makeSmall(key)
	}
}

// JSONString returns Bytes(key, size) base64-encoded into a string so
// the REST handler can emit it verbatim in a JSON field. Base64 is the
// honest way to make REST's payload comparable to gRPC's: gRPC ships
// raw binary, REST/JSON pays a ~33% expansion tax.
func JSONString(key, size string) string {
	return base64.StdEncoding.EncodeToString(Bytes(key, size))
}

func makeSmall(key string) []byte {
	// 256 bytes of pseudo-randomness seeded by the key.
	h := sha256.Sum256([]byte("small:" + key))
	out := make([]byte, 0, 256)
	for len(out) < 256 {
		nh := sha256.Sum256(append(h[:], byte(len(out))))
		out = append(out, nh[:]...)
	}
	return out[:256]
}

func makeLarge(key string) []byte {
	// ~4 KB of pseudo-randomness seeded by the key. We use sha256
	// rather than rand.Reader because the bench needs determinism.
	h := sha256.Sum256([]byte("large:" + key))
	out := make([]byte, 0, 4096)
	for len(out) < 4096 {
		nh := sha256.Sum256(append(h[:], byte(len(out))))
		out = append(out, nh[:]...)
	}
	return out[:4096]
}

// SizeForRequest returns a normalised size, defaulting to small. We
// trim and lower-case so headers/JSON case differences don't trip the
// bench.
func SizeForRequest(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case SizeLarge:
		return SizeLarge
	default:
		return SizeSmall
	}
}
