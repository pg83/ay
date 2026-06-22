package main

import (
	"strings"
	"unsafe"

	"github.com/zeebo/xxh3"
)

var (
	strDollar     TwoBitSet
	srcExtClasses []uint8
)

var internTable = struct {
	ids      *IntMap[STR]
	overflow map[string]STR
	los      []uint64
	strs     []string

	bytes *BumpAllocator[byte]
}{
	ids:      newIntMap[STR](1 << 16),
	overflow: make(map[string]STR),
	los:      make([]uint64, 1, 1<<16),
	strs:     make([]string, 1, 1<<16),
	bytes:    newBumpAllocator[byte](1 << 20),
}

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

func isCodegenProducingSrcID(id STR) bool {
	switch srcExtClassOf(id) {
	case srcExtProto, srcExtGztProto, srcExtFbs, srcExtFbs64, srcExtEv, srcExtCfgProto, srcExtRl6, srcExtRl, srcExtY, srcExtCppIn, srcExtCIn, srcExtSc, srcExtGperf, srcExtFlex:
		return true
	}

	return false
}

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

func (id STR) str() STR {
	return id
}

func (id STR) string() string {
	if strProbeEnabled {
		strProbeAt()
	}

	return internTable.strs[id]
}

func (id STR) String() string {
	return id.string()
}

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
