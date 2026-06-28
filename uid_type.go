package main

import (
	"encoding/base64"
	"encoding/binary"
)

const uidB64Len = 22

type UID struct {
	Hi uint64
	Lo uint64
}

func (u UID) raw() [16]byte {
	var b [16]byte
	binary.BigEndian.PutUint64(b[0:8], u.Hi)
	binary.BigEndian.PutUint64(b[8:16], u.Lo)

	return b
}

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

func (u UID) String() string {
	return u.string()
}

func (u UID) marshalJSON() ([]byte, error) {
	out := make([]byte, 0, uidB64Len+2)

	out = append(out, '"')
	out = u.appendB64(out)
	out = append(out, '"')

	return out, nil
}

func (u UID) MarshalJSON() ([]byte, error) {
	return u.marshalJSON()
}
