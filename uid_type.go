package main

import (
	"encoding/base64"
	"encoding/binary"
)

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

func (u UID) string() string {
	raw := u.raw()

	return base64.RawURLEncoding.EncodeToString(raw[:])
}
