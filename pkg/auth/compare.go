package auth

import (
	"crypto/sha256"
	"crypto/subtle"
)

// constantTimeEqualString compares two strings without leaking their length
// or contents via timing side channels.
//
// crypto/subtle.ConstantTimeCompare returns 0 immediately when the two byte
// slices have different lengths, which lets an attacker learn the secret's
// length by measuring response time. We mitigate this by hashing both inputs
// with SHA-256 first, so the constant-time compare always runs over a
// fixed-size 32-byte buffer regardless of input length.
//
// Note: SHA-256 itself is not constant-time across arbitrary input lengths,
// but the timing difference is dominated by I/O and the per-block cost is
// tiny relative to a network round trip. The important property — that the
// length and content of the secret are not revealed by a length-mismatch
// fast path — is preserved.
func constantTimeEqualString(a, b string) bool {
	ah := sha256.Sum256([]byte(a))
	bh := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ah[:], bh[:]) == 1
}
