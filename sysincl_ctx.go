package main

import (
	"sort"
	"strings"
)

// sysinclCtx owns the sysincl rule set and the indexes that drive include
// resolution overrides. Built once per scanner from a SysInclSet:
//   - mightClaim — a cheap, sound prefilter over the rule keys;
//   - sourceLookup — source-keyed records, matched inline;
//   - includerLookup — includer-keyed records, via the header-first merged index.
type sysinclCtx struct {
	set SysInclSet

	// keyBits/keyCI/ciGate/ciMaxLen back mightClaim: a "could any record map this
	// target" gate evaluated before the full lookup.
	keyBits  []bool          // case-sensitive keys, indexed by target STR
	keyCI    map[string]bool // case-insensitive keys (lowercased)
	ciGate   idBitSet[uint16]
	ciMaxLen int // longest CI key; longer targets cannot match (cheap reject)

	// merged folds all includer-keyed records into one header-keyed index.
	merged *mergedIncluderIndex
}

func newSysinclCtx(set SysInclSet) *sysinclCtx {
	c := &sysinclCtx{set: set}

	var csKeyIDs []STR

	for i := range set {
		rec := &set[i]

		for k := range rec.Mappings {
			if rec.CaseInsensitive {
				if c.keyCI == nil {
					c.keyCI = make(map[string]bool, len(rec.Mappings))
				}

				c.keyCI[k] = true
			} else {
				csKeyIDs = append(csKeyIDs, internString(k))
			}
		}
	}

	c.keyBits = make([]bool, internBound())

	for _, id := range csKeyIDs {
		c.keyBits[id] = true
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
				c.ciGate.add(uint16(x0)*l + uint16(x1))
			}
		}
	}

	c.merged = buildMergedIncluderIndex(set.includerKeyedRecords())

	return c
}

// mightClaim is a sound, cheap prefilter: a false result guarantees no sysincl
// record can map target, so the caller skips the full lookup.
func (c *sysinclCtx) mightClaim(target STR) bool {
	if int(target) < len(c.keyBits) && c.keyBits[target] {
		return true
	}

	if len(c.keyCI) != 0 {
		raw := target.String()

		if len(raw) > c.ciMaxLen {
			return false
		}

		if len(raw) >= 2 && !c.ciGate.has(uint16(raw[0])*uint16(len(raw))+uint16(raw[1])) {
			return false
		}

		return c.keyCI[strings.ToLower(raw)]
	}

	return false
}

// lookup resolves target's sysincl override for a file at sourceRel / includerRel
// (the scanner passes the includer's own path for both), unioning the source-keyed
// and includer-keyed results.
func (c *sysinclCtx) lookup(sourceRel, includerRel string, target STR) (paths []VFS, hasMultiTarget, claimed bool) {
	if !c.mightClaim(target) {
		return nil, false, false
	}

	srcMappings, srcMT, srcClaimed := c.sourceLookup(sourceRel, target)
	incMappings, incMT, incClaimed := c.includerLookup(includerRel, target)
	claimed = srcClaimed || incClaimed

	switch {
	case len(srcMappings) == 0:
		paths = incMappings
	case len(incMappings) == 0:
		paths = srcMappings
	default:
		out := make([]VFS, 0, len(srcMappings)+len(incMappings))
		out = append(out, srcMappings...)

	incLoop:
		for _, p := range incMappings {
			for _, q := range out {
				if p == q {
					continue incLoop
				}
			}

			out = append(out, p)
		}

		paths = out
	}

	hasMultiTarget = srcMT || incMT || len(paths) >= 2

	return paths, hasMultiTarget, claimed
}

func (c *sysinclCtx) sourceLookup(sourceRel string, target STR) ([]VFS, bool, bool) {
	header := target.String()

	var (
		out            []VFS
		found          bool
		hasMultiTarget bool
		seen           map[string]struct{}
	)

	for i := range c.set {
		rec := &c.set[i]

		if !rec.KeyBySource {
			continue
		}

		if rec.Filter != nil && !rec.Filter.match(sourceRel) {
			continue
		}

		paths, ok := rec.Mappings[recordQuery(rec, header)]

		if !ok {
			continue
		}

		found = true

		if rec.HasMultiTarget {
			count := 0

			for _, p := range paths {
				if p != "" {
					count++
				}
			}

			if count >= 2 {
				hasMultiTarget = true
			}
		}

		for _, p := range paths {
			if p == "" {
				continue
			}

			if seen == nil {
				seen = make(map[string]struct{}, 4)
			}

			if _, dup := seen[p]; dup {
				continue
			}

			seen[p] = struct{}{}
			out = append(out, Source(normalisePath(p)))
		}
	}

	return out, hasMultiTarget, found
}

func (c *sysinclCtx) includerLookup(includerRel string, target STR) ([]VFS, bool, bool) {
	rels, claimed, hasMultiTarget := c.merged.lookup(includerRel, target.String())

	return absifyRels(rels), hasMultiTarget, claimed
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

func absifyRels(rels []string) []VFS {
	if len(rels) == 0 {
		return nil
	}

	out := make([]VFS, 0, len(rels))

	for _, rel := range rels {
		out = append(out, Source(normalisePath(rel)))
	}

	return out
}

// includerContribution is one includer-keyed record's mapping for a header
// bucket, carrying the record's Filter so activeness is decided at query time.
type includerContribution struct {
	paths  []string
	filter *sourceFilter // nil = applies to every includer
	rawKey string        // the record's stored key (lowercase for CI records)
	order  int           // index in includerKeyed — preserves union order
	ci     bool
	multi  bool // record.HasMultiTarget
}

// mergedIncluderIndex folds ALL includer-keyed records into one header-keyed
// index built once, so an includer lookup is a single map probe plus a tiny
// bucket scan instead of a per-record fan-out. Keyed by ToLower(header); each
// bucket holds every record whose key folds to it, sorted by record order. A
// matched entry must (a) be active for the includer (filter), (b) match case
// (CI = whole bucket, non-CI = exact rawKey). This is the includer-keyed sysincl
// lookup — the scanner calls m.lookup directly, per resolve.
type mergedIncluderIndex struct {
	byLower map[string][]includerContribution
}

func buildMergedIncluderIndex(includerKeyed []*SysIncl) *mergedIncluderIndex {
	m := &mergedIncluderIndex{byLower: make(map[string][]includerContribution)}

	for order, rec := range includerKeyed {
		for k, paths := range rec.Mappings {
			lc := k

			if !rec.CaseInsensitive {
				lc = strings.ToLower(k)
			}

			m.byLower[lc] = append(m.byLower[lc], includerContribution{
				paths:  paths,
				filter: rec.Filter,
				rawKey: k,
				order:  order,
				ci:     rec.CaseInsensitive,
				multi:  rec.HasMultiTarget,
			})
		}
	}

	for _, bucket := range m.byLower {
		sort.Slice(bucket, func(i, j int) bool { return bucket[i].order < bucket[j].order })
	}

	return m
}

func (m *mergedIncluderIndex) lookup(includerPath, header string) ([]string, bool, bool) {
	bucket := m.byLower[strings.ToLower(header)]

	if bucket == nil {
		return nil, false, false
	}

	var (
		out            []string
		found          bool
		hasMultiTarget bool
		seen           map[string]struct{}
	)

	for i := range bucket {
		c := &bucket[i]

		if !c.ci && c.rawKey != header {
			continue
		}

		if c.filter != nil && !c.filter.match(includerPath) {
			continue
		}

		found = true

		if c.multi {
			count := 0

			for _, p := range c.paths {
				if p != "" {
					count++
				}
			}

			if count >= 2 {
				hasMultiTarget = true
			}
		}

		for _, p := range c.paths {
			if p == "" {
				continue
			}

			if seen == nil {
				seen = make(map[string]struct{}, 4)
			}

			if _, dup := seen[p]; dup {
				continue
			}

			seen[p] = struct{}{}
			out = append(out, p)
		}
	}

	return out, found, hasMultiTarget
}
