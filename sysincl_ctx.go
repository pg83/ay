package main

import (
	"strings"
)

const (
	ciUnseen CIMemoState = iota
	ciNo
	ciClaimed
)

type SysinclCtx struct {
	keyBits  BitSet
	keyCI    map[string]bool
	ciGate   BitSet
	ciMaxLen int
	ciMemo   TwoBitSet
	merged   *SysinclIndex
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

		for _, x0 := range caseVariants(k[0]) {
			for _, x1 := range caseVariants(k[1]) {
				c.ciGate.add(uint32(uint16(x0)*l + uint16(x1)))
			}
		}
	}

	c.merged = buildSysinclIndex(set)

	return c
}

type CIMemoState uint8

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

func (c *SysinclCtx) lookup(path string, target STR) ([]VFS, bool, bool) {
	if !c.mightClaim(target) {
		return nil, false, false
	}

	paths, claimed, hasMultiTarget := c.merged.lookup(path, target)

	return paths, hasMultiTarget || len(paths) >= 2, claimed
}

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

type SysinclContribution struct {
	paths    []VFS
	filter   *SourceFilter
	rawKeyID STR
	ci       bool
	multi    bool
}

type SysinclIndex struct {
	byLower    map[string]int32
	buckets    [][]SysinclContribution
	byID       *IntValueMap[int32]
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

		for i := range rec.pairs {
			if rec.pairs[i].key == 0 {
				internStr(rec.pairs[i].keyCI)
			}
		}

		deduper.reset()

		for i := len(rec.pairs) - 1; i >= 0; i-- {
			p := &rec.pairs[i]
			id := p.key

			if id == 0 {
				id = internStr(p.keyCI)
			}

			if !deduper.add(id.strID()) {
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

	return m
}

func (m *SysinclIndex) lookup(path string, target STR) ([]VFS, bool, bool) {
	bi := m.byID.get(uint64(target))
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
