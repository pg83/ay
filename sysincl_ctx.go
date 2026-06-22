package main

import (
	"strings"
)

// sysinclCtx owns the sysincl rule set's lookup indexes. Built once per scanner:
//   - mightClaim — a cheap, sound prefilter over the rule keys;
//   - merged — ALL records (source- and includer-keyed) in one header-first index.
//
// The scanner matches every filter against the includer's own path, so the
// source/includer rule split falls out of each record's filter. Result order is
// irrelevant (the gate sorts node inputs), so both rule kinds share one index.
type SysinclCtx struct {
	// keyBits/keyCI/ciGate/ciMaxLen back mightClaim: a "could any record map this
	// target" gate evaluated before the full lookup.
	keyBits  BitSet          // case-sensitive keys, indexed by target STR
	keyCI    map[string]bool // case-insensitive keys (lowercased)
	ciGate   BitSet
	ciMaxLen int // longest CI key; longer targets cannot match (cheap reject)

	// First-touch memo over the CI arm, indexed by target STR: the rule set is
	// immutable per scanner, so mightClaim(target) is constant per id. Repeated
	// targets answer from one 2-bit probe without a string view. Cell values:
	// ciUnseen / ciNo / ciClaimed.
	ciMemo TwoBitSet

	merged *SysinclIndex
}

func newSysinclCtx(set SysInclSet) *SysinclCtx {
	c := &SysinclCtx{}

	for i := range set {
		rec := &set[i]

		for _, p := range rec.pairs {
			if p.key != 0 {
				c.keyBits.add(uint32(p.key))

				continue
			}

			if c.keyCI == nil {
				c.keyCI = make(map[string]bool, len(rec.pairs))
			}

			c.keyCI[p.keyCI] = true
		}
	}

	for k := range c.keyCI {
		if len(k) > c.ciMaxLen {
			c.ciMaxLen = len(k)
		}

		if len(k) < 2 {
			continue
		}

		l := uint16(len(k))

		// ciGate keys on uint16(raw[0])*len + uint16(raw[1]) over the RAW
		// target bytes (no ToLower) — built over both case variants of each CI key's
		// first two bytes — any CI match passes, a miss proves the target is not
		// a CI header. The ciMaxLen gate runs first, so the uint16 key never overflows.
			for _, x0 := range caseVariants(k[0]) {
				for _, x1 := range caseVariants(k[1]) {
				c.ciGate.add(uint32(uint16(x0)*l + uint16(x1)))
			}
		}
	}

	c.merged = buildSysinclIndex(set)

	return c
}

// CIMemoState is a ciMemo cell value; ciUnseen doubles as TwoBitSet's zero.
type CIMemoState uint8

const (
	ciUnseen CIMemoState = iota
	ciNo
	ciClaimed
)

// mightClaim is a sound, cheap prefilter: a false result guarantees no record
// can map target, so the caller skips the full lookup.
func (c *SysinclCtx) mightClaim(target STR) bool {
	if c.keyBits.has(uint32(target)) {
		return true
	}

	if len(c.keyCI) != 0 {
		if cell := CIMemoState(c.ciMemo.get(uint32(target))); cell != ciUnseen {
			return cell == ciClaimed
		}

		yes := c.ciClaims(target)

		if yes {
			c.ciMemo.set(uint32(target), uint8(ciClaimed))
		} else {
			c.ciMemo.set(uint32(target), uint8(ciNo))
		}

		return yes
	}

	return false
}

// ciClaims is the memo-miss arm of mightClaim: the only place the CI check
// materializes the target string. A claim also binds the target id to its
// merged-index bucket (byID) in the same touch, so lookup never needs the string
// for case-variant targets.
func (c *SysinclCtx) ciClaims(target STR) bool {
	raw := target.string()

	if len(raw) > c.ciMaxLen {
		return false
	}

	if len(raw) >= 2 && !c.ciGate.has(uint32(uint16(raw[0])*uint16(len(raw))+uint16(raw[1]))) {
		return false
	}

	lower := strings.ToLower(raw)

	if !c.keyCI[lower] {
		return false
	}

	bi, ok := c.merged.byLower[lower]

	if !ok {
		throwFmt("sysincl: CI key %q has no merged-index bucket", lower)
	}

	c.merged.byID.put(uint64(target), bi)

	return true
}

// lookup resolves target's sysincl override for a file at path (the scanner
// passes the includer's own path) via the merged header-first index.
func (c *SysinclCtx) lookup(path string, target STR) ([]VFS, bool, bool) {
	if !c.mightClaim(target) {
		return nil, false, false
	}

	paths, claimed, hasMultiTarget := c.merged.lookup(path, target)

	return paths, hasMultiTarget || len(paths) >= 2, claimed
}

// caseVariants returns b plus, for an ASCII letter, its opposite-case form — the
// byte values a CI include target could carry at that position.
func caseVariants(b byte) []byte {
	switch {
	case b >= 'a' && b <= 'z':
		return []byte{b, b - 32}
	case b >= 'A' && b <= 'Z':
		return []byte{b, b + 32}
	default:
		return []byte{b}
	}
}

// sysinclContribution is one record's mapping for a header bucket, carrying the
// record's Filter so activeness is decided per query against the includer path.
type SysinclContribution struct {
	paths    []VFS
	filter   *SourceFilter // nil = applies to every path
	rawKeyID STR           // the record's stored key id (lowered for CI records)
	ci       bool
	multi    bool // record.HasMultiTarget
}

// sysinclIndex folds ALL records (source- and includer-keyed) into one
// header-keyed index, so a lookup is a single map probe plus a tiny bucket scan
// instead of a per-record fan-out. Buckets group records whose key folds to one
// ToLower form, in declaration order; a matched entry must (a) match the path
// (filter), (b) match case (CI = whole bucket, non-CI = exact rawKeyID). Lookups
// address buckets purely by target id via byID (CS and lowered CI keys bound at
// build, case variants at ciClaims' first touch), so the target string is never
// materialized. byLower exists for the build and that first-touch fill.
type SysinclIndex struct {
	byLower map[string]int32
	buckets [][]SysinclContribution
	byID    *IntValueMap[int32]

	// outScratch backs multi-contribution lookup results; the caller (resolve)
	// consumes the slice before the next lookup. A single matching contribution
	// returns its paths slice directly, allocation-free.
	outScratch []VFS
}

func buildSysinclIndex(set SysInclSet) *SysinclIndex {
	m := &SysinclIndex{byLower: make(map[string]int32), byID: newIntValueMap[int32](4096)}

	bucketFor := func(lc string) int32 {
		if i, ok := m.byLower[lc]; ok {
			return i
		}

		i := int32(len(m.buckets))
		m.buckets = append(m.buckets, nil)
		m.byLower[lc] = i

		return i
	}

	for order := range set {
		rec := &set[order]

		// Intra-record duplicate headers carry last-wins semantics: keep only
		// each key's LAST pair, via a reverse scan over the epoch deduper
		// (CS arm keyed by the interned key, CI arm by the lowered intern id).
		deduper.reset()

		for i := len(rec.pairs) - 1; i >= 0; i-- {
			p := &rec.pairs[i]
			id := p.key

			if id == 0 {
				id = internStr(p.keyCI)
			}

			if !deduper.add(VFS(id) << 1) {
				// Tombstone the earlier duplicate.
				p.paths = nil
				p.key = 0
				p.keyCI = ""
			}
		}

		for _, p := range rec.pairs {
			if p.key == 0 && p.keyCI == "" {
				continue
			}

			if p.key != 0 {
				bi := bucketFor(strings.ToLower(p.key.string()))
				m.buckets[bi] = append(m.buckets[bi], SysinclContribution{
					paths:    p.paths,
					filter:   rec.Filter,
					rawKeyID: p.key,
					ci:       false,
					multi:    rec.HasMultiTarget,
				})
				m.byID.put(uint64(p.key), bi)

				continue
			}

			ciID := internStr(p.keyCI)
			bi := bucketFor(p.keyCI)
			m.buckets[bi] = append(m.buckets[bi], SysinclContribution{
				paths:    p.paths,
				filter:   rec.Filter,
				rawKeyID: ciID,
				ci:       true,
				multi:    rec.HasMultiTarget,
			})
			m.byID.put(uint64(ciID), bi)
		}
	}

	// Buckets are already in record order: the build loop only appends.

	return m
}

func (m *SysinclIndex) lookup(path string, target STR) ([]VFS, bool, bool) {
	bi := m.byID.get(uint64(target))

	if bi == nil {
		return nil, false, false
	}

	bucket := m.buckets[*bi]

	var (
		out            []VFS
		found          bool
		hasMultiTarget bool
		single         []VFS
		singleMulti    bool
	)

	for i := range bucket {
		c := &bucket[i]

		if !c.ci && c.rawKeyID != target {
			continue
		}

		if c.filter != nil && !c.filter.match(path) {
			continue
		}

		cMulti := c.multi && len(c.paths) >= 2

		if !found {
			// First (usually only) match: hand back its paths directly.
			found = true
			single = c.paths
			singleMulti = cMulti

			continue
		}

		if out == nil {
			out = append(m.outScratch[:0], single...)
			hasMultiTarget = singleMulti
		}

		if cMulti {
			hasMultiTarget = true
		}

		out = append(out, c.paths...)
	}

	if out == nil {
		return single, found, singleMulti
	}

	m.outScratch = out

	return out, found, hasMultiTarget
}
