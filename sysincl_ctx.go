package main

import (
	"sort"
	"strings"
)

// sysinclCtx owns the sysincl rule set's lookup indexes. Built once per scanner:
//   - mightClaim — a cheap, sound prefilter over the rule keys;
//   - merged — ALL records (source- and includer-keyed) in one header-first index.
//
// The scanner matches every filter against the includer's own path, so the
// source/includer rule split needs no separate handling — it falls out of each
// record's filter. Result order is irrelevant (the gate sorts node inputs), so
// both rule kinds share one index.
type SysinclCtx struct {
	// keyBits/keyCI/ciGate/ciMaxLen back mightClaim: a "could any record map this
	// target" gate evaluated before the full lookup.
	keyBits  BitSet          // case-sensitive keys, indexed by target STR
	keyCI    map[string]bool // case-insensitive keys (lowercased)
	ciGate   BitSet
	ciMaxLen int // longest CI key; longer targets cannot match (cheap reject)

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

		// sysinclCIGate keys on uint16(raw[0])*len + uint16(raw[1]) over the RAW
		// target bytes (no ToLower) — built over both case variants of each CI key's
		// first two bytes, so any case-insensitive match passes and a miss proves
		// the target is not a CI header. The ciMaxLen length gate runs first, so
		// len <= 45 here and the uint16 key never overflows.
		for _, x0 := range caseVariants(k[0]) {
			for _, x1 := range caseVariants(k[1]) {
				c.ciGate.add(uint32(uint16(x0)*l + uint16(x1)))
			}
		}
	}

	c.merged = buildSysinclIndex(set)

	return c
}

// mightClaim is a sound, cheap prefilter: a false result guarantees no sysincl
// record can map target, so the caller skips the full lookup.
func (c *SysinclCtx) mightClaim(target STR) bool {
	if c.keyBits.has(uint32(target)) {
		return true
	}

	if len(c.keyCI) != 0 {
		raw := target.string()

		if len(raw) > c.ciMaxLen {
			return false
		}

		if len(raw) >= 2 && !c.ciGate.has(uint32(uint16(raw[0])*uint16(len(raw))+uint16(raw[1]))) {
			return false
		}

		return c.keyCI[strings.ToLower(raw)]
	}

	return false
}

// lookup resolves target's sysincl override for a file at path (the scanner uses
// the includer's own path) via the merged header-first index over all records.
func (c *SysinclCtx) lookup(path string, target STR) ([]VFS, bool, bool) {
	if !c.mightClaim(target) {
		return nil, false, false
	}

	paths, claimed, hasMultiTarget := c.merged.lookup(path, target.string())

	return paths, hasMultiTarget || len(paths) >= 2, claimed
}

// caseVariants returns b plus, for an ASCII letter, its opposite-case form — the
// byte values a case-insensitive include target could carry at that position.
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

// sysinclContribution is one sysincl record's mapping for a header bucket,
// carrying the record's Filter so activeness is decided per query against the
// includer path.
type SysinclContribution struct {
	paths  []VFS
	filter *SourceFilter // nil = applies to every path
	rawKey string        // the record's stored key (lowercase for CI records)
	order  int           // index in the rule set
	ci     bool
	multi  bool // record.HasMultiTarget
}

// sysinclIndex folds ALL sysincl records (source- and includer-keyed) into one
// header-keyed index built once, so a lookup is a single map probe plus a tiny
// bucket scan instead of a per-record fan-out. Keyed by ToLower(header); each
// bucket holds every record whose key folds to it, sorted by record order. A
// matched entry must (a) match the path (filter), (b) match case (CI = whole
// bucket, non-CI = exact rawKey).
type SysinclIndex struct {
	byLower map[string][]SysinclContribution
}

func buildSysinclIndex(set SysInclSet) *SysinclIndex {
	m := &SysinclIndex{byLower: make(map[string][]SysinclContribution)}

	for order := range set {
		rec := &set[order]

		// Intra-record duplicate headers carry last-wins semantics (the old
		// per-record map staging overwrote): keep only each key's LAST pair.
		// Membership rides the global epoch deduper (CS arm keyed by the
		// interned key, CI arm by the lowered key's intern id), reverse scan.
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
				raw := p.key.string()
				lc := strings.ToLower(raw)
				m.byLower[lc] = append(m.byLower[lc], SysinclContribution{
					paths:  p.paths,
					filter: rec.Filter,
					rawKey: raw,
					order:  order,
					ci:     false,
					multi:  rec.HasMultiTarget,
				})

				continue
			}

			m.byLower[p.keyCI] = append(m.byLower[p.keyCI], SysinclContribution{
				paths:  p.paths,
				filter: rec.Filter,
				rawKey: p.keyCI,
				order:  order,
				ci:     true,
				multi:  rec.HasMultiTarget,
			})
		}
	}

	for _, bucket := range m.byLower {
		sort.Slice(bucket, func(i, j int) bool { return bucket[i].order < bucket[j].order })
	}

	return m
}

func (m *SysinclIndex) lookup(path, header string) ([]VFS, bool, bool) {
	bucket := m.byLower[strings.ToLower(header)]

	if bucket == nil {
		return nil, false, false
	}

	var (
		out            []VFS
		found          bool
		hasMultiTarget bool
	)

	for i := range bucket {
		c := &bucket[i]

		if !c.ci && c.rawKey != header {
			continue
		}

		if c.filter != nil && !c.filter.match(path) {
			continue
		}

		found = true

		if c.multi && len(c.paths) >= 2 {
			hasMultiTarget = true
		}

		out = append(out, c.paths...)
	}

	return out, found, hasMultiTarget
}
