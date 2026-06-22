package main

import (
	"strings"
	"unsafe"

	"github.com/zeebo/xxh3"
)

var (
	// strDollar memoizes "does the interned string contain '$'" per STR id — the
	// macro-expansion fast-path predicate. Immutable, so constant per id.
	strDollar TwoBitSet
	// srcExtClasses memoizes SrcExtClass per STR id, first-touch.
	srcExtClasses []uint8
)

// internTable maps strings to dense STR ids keyed by the hi 64 bits of xxh3-128,
// a hit verified by a uint64 lo compare. A hi-collision falls through to the exact
// string-keyed overflow map, keeping identity exact off the hot path.
var internTable = struct {
	ids      *IntMap[STR]   // hi 64 bits of xxh3-128(s) → STR, identity-hashed
	overflow map[string]STR // exact fallback for the rare hi-collision
	los      []uint64       // lo 64 bits of xxh3-128(s) per STR; also the per-path hash in node UIDs
	strs     []string
	// bytes backs strings interned from transient byte views, batched into arena
	// chunks. Committed bytes are never rewritten, as unsafe.String requires.
	bytes *BumpAllocator[byte]
}{
	ids:      newIntMap[STR](1 << 16),
	overflow: make(map[string]STR),
	los:      make([]uint64, 1, 1<<16),
	strs:     make([]string, 1, 1<<16),
	bytes:    newBumpAllocator[byte](1 << 20),
}

// internOwnedCopy copies b into the byte arena, returning a view over the
// committed region.
func internOwnedCopy(b []byte) string {
	n := len(b)

	if n == 0 {
		return ""
	}

	block := internTable.bytes.alloc(n)
	copy(block, b)
	internTable.bytes.commit(n)

	return unsafe.String(&block[0], n)
}

type STR uint32

// internAppend allocates the next STR slot for s, recording its lo half.
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

		// hi-collision: exact string-keyed fallback.
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

		s := internOwnedCopy(b)
		id := internAppend(s, h.Lo)
		internTable.overflow[s] = id

		return id
	}

	id := internAppend(internOwnedCopy(b), h.Lo)
	internTable.ids.put(h.Hi, id)

	return id
}

// DollarMemoState is a strDollar cell value; dollarUnseen is the zero.
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

// SrcExtClass is a srcExtClasses cell: the suffix triage of a src token.
type SrcExtClass uint8

const (
	srcExtUnseen SrcExtClass = iota
	srcExtRegular
	srcExtProto
	srcExtGztProto
	srcExtFbs
	srcExtFbs64
	srcExtEv
	srcExtRl6
	srcExtRl
	srcExtY
	srcExtCppIn
	srcExtCIn
	srcExtHIn
	srcExtSc
	srcExtCfgProto
	srcExtGperf
	srcExtFlex
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
	case strings.HasSuffix(s, ".gztproto"):
		return srcExtGztProto
	case strings.HasSuffix(s, ".proto"):
		return srcExtProto
	case strings.HasSuffix(s, ".fbs64"):
		return srcExtFbs64
	case strings.HasSuffix(s, ".fbs"):
		return srcExtFbs
	case strings.HasSuffix(s, ".ev"):
		return srcExtEv
	case strings.HasSuffix(s, ".rl6"):
		return srcExtRl6
	case strings.HasSuffix(s, ".rl"):
		return srcExtRl
	case strings.HasSuffix(s, ".y"), strings.HasSuffix(s, ".ypp"):
		return srcExtY
	case strings.HasSuffix(s, ".cpp.in"):
		return srcExtCppIn
	case strings.HasSuffix(s, ".c.in"):
		return srcExtCIn
	case strings.HasSuffix(s, ".h.in"):
		return srcExtHIn
	case strings.HasSuffix(s, ".sc"):
		return srcExtSc
	case strings.HasSuffix(s, ".cfgproto"):
		return srcExtCfgProto
	case strings.HasSuffix(s, ".gperf"):
		return srcExtGperf
	case strings.HasSuffix(s, ".lpp"),
		strings.HasSuffix(s, ".lex"),
		strings.HasSuffix(s, ".l"):
		return srcExtFlex
	default:
		return srcExtRegular
	}
}

// isCodegenProducingSrcID is isCodegenProducingSrc in id space.
func isCodegenProducingSrcID(id STR) bool {
	switch srcExtClassOf(id) {
	case srcExtProto, srcExtGztProto, srcExtFbs, srcExtFbs64, srcExtEv, srcExtCfgProto, srcExtRl6, srcExtRl, srcExtY, srcExtCppIn, srcExtCIn, srcExtSc, srcExtGperf, srcExtFlex:
		return true
	}

	return false
}

// internedBytes is the lookup-only twin of internBytes: probes without inserting.
func internedBytes(b []byte) STR {
	h := xxh3.Hash128(b)

	if p := internTable.ids.get(h.Hi); p != nil {
		if internTable.los[*p] == h.Lo {
			return *p
		}

		if oid, ok := internTable.overflow[string(b)]; ok {
			return oid
		}
	}

	return 0
}

// str returns the STR itself — the identity arm of the uniform X.str() shared by
// ARG/ENV/VFS/TOK.
func (id STR) str() STR {
	return id
}

func (id STR) string() string {
	if strProbeEnabled {
		strProbeAt()
	}

	return internTable.strs[id]
}

// String implements fmt.Stringer; internal code calls string().
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

// interned is the read-only intern probe: 0 means never interned.
func interned(s string) STR {
	h := xxh3.HashString128(s)

	if p := internTable.ids.get(h.Hi); p != nil {
		if internTable.los[*p] == h.Lo {
			return *p
		}

		if oid, ok := internTable.overflow[s]; ok {
			return oid
		}
	}

	return 0
}

func internBound() uint32 {
	return uint32(len(internTable.strs))
}
