package main

import (
	"strings"

	"github.com/zeebo/xxh3"
)

var (
	// strDollar is a first-touch memo over "does the interned string contain '$'",
	// indexed by STR id — the macro-expansion fast-path predicate (expandStmtTokensSTR
	// and friends). Tokens repeat heavily across ya.makes; an interned string is
	// immutable, so the answer is constant per id. Same single-writer contract as
	// internTable.
	strDollar TwoBitSet
	// srcExtClasses memoizes SrcExtClass per STR id, first-touch (an interned
	// string is immutable, so the class is constant per id). Same single-writer
	// contract as internTable.
	srcExtClasses []uint8
)

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
	ids:      newIntMap[STR](1 << 16),
	overflow: make(map[string]STR),
	los:      make([]uint64, 1, 1<<16),
	strs:     make([]string, 1, 1<<16),
}

type STR uint32

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

// DollarMemoState is a strDollar cell value; dollarUnseen doubles as
// TwoBitSet's zero.
type DollarMemoState uint8

const (
	dollarUnseen DollarMemoState = iota
	dollarAbsent
	dollarPresent
)

func strHasDollar(id STR) bool {
	if cell := DollarMemoState(strDollar.get(uint32(id))); cell != dollarUnseen {
		return cell == dollarPresent
	}

	yes := strings.Contains(id.string(), "$")

	if yes {
		strDollar.set(uint32(id), uint8(dollarPresent))
	} else {
		strDollar.set(uint32(id), uint8(dollarAbsent))
	}

	return yes
}

// SrcExtClass is a srcExtClasses cell: the suffix triage of a src token,
// shared by the SRCS collect arm, collectModule's .ev/.proto/.fbs pass and
// genModule's codegen-producing gates. srcExtUnseen doubles as the zero value.
type SrcExtClass uint8

const (
	srcExtUnseen SrcExtClass = iota
	srcExtRegular
	srcExtProto
	srcExtFbs
	srcExtEv
	srcExtRl6
	srcExtRl
	srcExtY
	srcExtCppIn
	srcExtCIn
	srcExtHIn
)

func srcExtClassOf(id STR) SrcExtClass {
	if int(id) < len(srcExtClasses) {
		if c := SrcExtClass(srcExtClasses[id]); c != srcExtUnseen {
			return c
		}
	}

	c := classifySrcExt(id.string())

	for int(id) >= len(srcExtClasses) {
		grown := len(srcExtClasses) * 2

		if grown <= int(id) {
			grown = int(id) + 1
		}

		next := make([]uint8, grown)
		copy(next, srcExtClasses)
		srcExtClasses = next
	}

	srcExtClasses[id] = uint8(c)

	return c
}

func classifySrcExt(s string) SrcExtClass {
	switch {
	case strings.HasSuffix(s, ".proto"):
		return srcExtProto
	case strings.HasSuffix(s, ".fbs"):
		return srcExtFbs
	case strings.HasSuffix(s, ".ev"):
		return srcExtEv
	case strings.HasSuffix(s, ".rl6"):
		return srcExtRl6
	case strings.HasSuffix(s, ".rl"):
		return srcExtRl
	case strings.HasSuffix(s, ".y"):
		return srcExtY
	case strings.HasSuffix(s, ".cpp.in"):
		return srcExtCppIn
	case strings.HasSuffix(s, ".c.in"):
		return srcExtCIn
	case strings.HasSuffix(s, ".h.in"):
		return srcExtHIn
	default:
		return srcExtRegular
	}
}

// isCodegenProducingSrcID is isCodegenProducingSrc in id space (the memoized
// class) — the same eight-extension set.
func isCodegenProducingSrcID(id STR) bool {
	switch srcExtClassOf(id) {
	case srcExtProto, srcExtFbs, srcExtEv, srcExtRl6, srcExtRl, srcExtY, srcExtCppIn, srcExtCIn:
		return true
	}

	return false
}

// internedBytes is the lookup-only twin of internBytes: it probes for b without
// inserting. The overflow probe's string(b) conversion allocates only on a
// hi-collision (essentially never).
func internedBytes(b []byte) *STR {
	h := xxh3.Hash128(b)

	if p := internTable.ids.get(h.Hi); p != nil {
		if internTable.los[*p] == h.Lo {
			id := *p

			return &id
		}

		if oid, ok := internTable.overflow[string(b)]; ok {
			return &oid
		}
	}

	return nil
}

// str returns the STR itself — the identity arm of the uniform X.str() STR
// conversion shared by ARG/ENV/VFS/TOK, so generic cmd-arg assembly can box any
// interned token the same way.
func (id STR) str() STR {
	return id
}

func (id STR) string() string {
	if strProbeEnabled {
		strProbeAt()
	}

	return internTable.strs[id]
}

// String implements fmt.Stringer — the fmt machinery finds it by name;
// internal code calls string().
func (id STR) String() string {
	return id.string()
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
