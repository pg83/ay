package main

import (
	"github.com/zeebo/xxh3"
)

type STR uint32

// internTable maps strings to dense STR ids without a string-keyed map on the
// hot path. The lookup map is keyed by the high 64 bits of the xxh3-128 of the
// string; los holds the low 64 bits per STR, so a hit is verified by a uint64
// compare rather than a string compare. A hi-collision (distinct strings sharing
// the hi half — ~1e-8 at this scale) is detected by the lo mismatch and resolved
// through the exact string-keyed overflow map, so identity is exact (no 128-bit
// false-merge) while the hot path pays only an 8-byte-key map probe.
var internTable = struct {
	ids      *IntMap[STR]   // hi 64 bits of xxh3-128(s) → STR, identity-hashed (hi is already a hash)
	overflow map[string]STR // exact fallback for the rare hi-collision
	los      []uint64       // low 64 bits of xxh3-128(s), indexed by STR; also the per-path hash mixed into node UIDs
	strs     []string
}{
	ids:      NewIntMap[STR](1 << 16),
	overflow: make(map[string]STR),
	los:      make([]uint64, 1, 1<<16),
	strs:     make([]string, 1, 1<<16),
}

// internAppend allocates the next STR slot for s, recording its lo half (the
// collision-verify key, reused as the per-path hash in node UIDs).
func internAppend(s string, lo uint64) STR {
	id := STR(len(internTable.strs))
	internTable.strs = append(internTable.strs, s)
	internTable.los = append(internTable.los, lo)

	return id
}

func internStr(s string) STR {
	h := xxh3.HashString128(s)

	if p := internTable.ids.get(h.Hi); p != nil {
		if internTable.los[*p] == h.Lo {
			return *p
		}

		// hi-collision: distinct strings share h.Hi; fall back to an exact
		// string-keyed lookup (essentially never populated).
		if oid, ok := internTable.overflow[s]; ok {
			return oid
		}

		id := internAppend(s, h.Lo)
		internTable.overflow[s] = id

		return id
	}

	id := internAppend(s, h.Lo)
	internTable.ids.put(h.Hi, id)

	return id
}

func internBytes(b []byte) STR {
	h := xxh3.Hash128(b)

	if p := internTable.ids.get(h.Hi); p != nil {
		if internTable.los[*p] == h.Lo {
			return *p
		}

		if oid, ok := internTable.overflow[string(b)]; ok {
			return oid
		}

		id := internAppend(string(b), h.Lo)
		internTable.overflow[string(b)] = id

		return id
	}

	id := internAppend(string(b), h.Lo)
	internTable.ids.put(h.Hi, id)

	return id
}

// str returns the STR itself — the identity arm of the uniform X.str() STR
// conversion shared by ARG/ENV/VFS/TOK, so generic cmd-arg assembly can box any
// interned token the same way.
func (id STR) str() STR {
	return id
}

func (id STR) String() string {
	return internTable.strs[id]
}

// internStrs interns a []string into a []STR (nil for empty).
func internStrs(ss []string) []STR {
	if len(ss) == 0 {
		return nil
	}

	out := make([]STR, len(ss))

	for i, s := range ss {
		out[i] = internStr(s)
	}

	return out
}

func interned(s string) *STR {
	h := xxh3.HashString128(s)

	if p := internTable.ids.get(h.Hi); p != nil {
		if internTable.los[*p] == h.Lo {
			id := *p

			return &id
		}

		if oid, ok := internTable.overflow[s]; ok {
			return &oid
		}
	}

	return nil
}

func internBound() uint32 {
	return uint32(len(internTable.strs))
}
