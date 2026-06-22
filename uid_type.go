package main

import (
	"encoding/base64"
	"encoding/binary"
)

// UID is a node's 128-bit content address (xxh3-128 of its canonical bytes),
// carried as two uint64 halves: equality is two integer compares, it allocates
// nothing, and it makes a cheap map hash (the value is already a hash). The
// base64 text form exists only at the JSON boundary.
type UID struct {
	Hi uint64
	Lo uint64
}

const uidB64Len = 22 // base64.RawURLEncoding.EncodedLen(16)

func (u UID) raw() [16]byte {
	var b [16]byte
	binary.BigEndian.PutUint64(b[0:8], u.Hi)
	binary.BigEndian.PutUint64(b[8:16], u.Lo)

	return b
}

// appendB64 appends the 22-char base64 text (no quotes) without allocating —
// the encode lands in a stack array first.
func (u UID) appendB64(buf []byte) []byte {
	raw := u.raw()
	var enc [uidB64Len]byte
	base64.RawURLEncoding.Encode(enc[:], raw[:])

	return append(buf, enc[:]...)
}

func (u UID) string() string {
	raw := u.raw()

	return base64.RawURLEncoding.EncodeToString(raw[:])
}

// String implements fmt.Stringer; internal code calls string().
func (u UID) String() string {
	return u.string()
}

// MarshalJSON emits the quoted base64 text. Used only by the stdlib encoder; the
// production writer uses appendUID for identical bytes without the allocation.
func (u UID) marshalJSON() ([]byte, error) {
	out := make([]byte, 0, uidB64Len+2)
	out = append(out, '"')
	out = u.appendB64(out)
	out = append(out, '"')

	return out, nil
}

// MarshalJSON implements json.Marshaler; internal code calls marshalJSON().
func (u UID) MarshalJSON() ([]byte, error) {
	return u.marshalJSON()
}
