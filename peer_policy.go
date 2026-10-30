package main

import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"
)

type PeerPolicyPattern struct {
	prefix   string
	prefixes []string
	contains string
	re       *regexp.Regexp
	exact    bool
	suffix   bool
}

func literalPrefixes(re *syntax.Regexp) []string {
	switch re.Op {
	case syntax.OpLiteral:
		return []string{string(re.Rune)}
	case syntax.OpCapture:
		return literalPrefixes(re.Sub[0])
	case syntax.OpAlternate:
		var out []string

		for _, sub := range re.Sub {
			prefixes := literalPrefixes(sub)

			if len(prefixes) == 0 {
				return nil
			}

			out = append(out, prefixes...)
		}

		return out
	case syntax.OpConcat:
		head := literalPrefixes(re.Sub[0])

		if len(head) == 1 && len(re.Sub) > 1 && (re.Sub[0].Op == syntax.OpLiteral || re.Sub[0].Op == syntax.OpCapture && re.Sub[0].Sub[0].Op == syntax.OpLiteral) {
			if tail := literalPrefixes(&syntax.Regexp{Op: syntax.OpConcat, Sub: re.Sub[1:]}); len(tail) > 0 {
				for i := range tail {
					tail[i] = head[0] + tail[i]
				}

				return tail
			}
		}

		return head
	}

	return nil
}

func peerPolicyLiteral(value string, exact bool) (string, bool) {
	var b strings.Builder

	for i := 0; i < len(value); i++ {
		c := value[i]

		if c == '\\' && i+1 < len(value) && strings.IndexByte("-_./", value[i+1]) >= 0 {
			i++
			c = value[i]
		} else if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '/' || c == '-' || c == '_' || c == '.' && !exact) {
			return "", false
		}

		b.WriteByte(c)
	}

	return b.String(), true
}

func peerPolicyPattern(pattern string) PeerPolicyPattern {
	pattern = strings.TrimPrefix(pattern, "^")

	if pattern == ".*" {
		return PeerPolicyPattern{}
	}

	value, exact := strings.CutSuffix(pattern, "$")

	if literal, ok := peerPolicyLiteral(value, exact); ok {
		return PeerPolicyPattern{prefix: literal, exact: exact}
	}

	if head, ok := strings.CutSuffix(value, ".*"); ok {
		if literal, ok := peerPolicyLiteral(head, false); ok {
			return PeerPolicyPattern{prefix: literal}
		}
	}

	if tail, ok := strings.CutPrefix(value, ".*"); ok {
		if literal, ok := peerPolicyLiteral(tail, exact); ok && literal != "" {
			return PeerPolicyPattern{contains: literal, suffix: exact}
		}
	}

	compiled := PeerPolicyPattern{re: regexp.MustCompile("^" + pattern)}

	if parsed, err := syntax.Parse(pattern, syntax.Perl); err == nil {
		for _, prefix := range literalPrefixes(parsed.Simplify()) {
			if prefix != "" {
				compiled.prefixes = append(compiled.prefixes, prefix)
			}
		}
	}

	return compiled
}

func (p PeerPolicyPattern) matches(dir string) bool {
	switch {
	case p.suffix:
		return strings.HasSuffix(dir, p.contains)
	case p.contains != "":
		return strings.Contains(dir, p.contains)
	}

	return strings.HasPrefix(dir, p.prefix) && (p.re == nil || p.re.MatchString(dir))
}

type PeerPolicyRule struct {
	from  PeerPolicyPattern
	to    PeerPolicyPattern
	allow bool
}

type peerPolicySide struct {
	darts   *Darts
	prefix  [][]int
	exact   [][]int
	loose   []int
	any     []int
	anySet  BitSet
	matches DenseMap[STR, []int]
}

type peerPolicySideBuilder struct {
	keys   []string
	index  map[string]int
	prefix [][]int
	exact  [][]int
	loose  []int
	any    []int
}

func (b *peerPolicySideBuilder) key(prefix string) int {
	if i, ok := b.index[prefix]; ok {
		return i
	}

	i := len(b.keys)

	b.keys = append(b.keys, prefix)
	b.prefix = append(b.prefix, nil)
	b.exact = append(b.exact, nil)
	b.index[prefix] = i

	return i
}

func (b *peerPolicySideBuilder) place(index int, pattern PeerPolicyPattern) {
	switch {
	case pattern.contains != "":
		b.loose = append(b.loose, index)
	case pattern.exact:
		k := b.key(pattern.prefix)

		b.exact[k] = append(b.exact[k], index)
	case pattern.re != nil && len(pattern.prefixes) > 0:
		for _, prefix := range pattern.prefixes {
			k := b.key(prefix)

			b.prefix[k] = append(b.prefix[k], index)
		}
	case pattern.prefix == "" && pattern.re == nil:
		b.any = append(b.any, index)
	case pattern.prefix == "":
		b.loose = append(b.loose, index)
	default:
		k := b.key(pattern.prefix)

		b.prefix[k] = append(b.prefix[k], index)
	}
}

func (b *peerPolicySideBuilder) side() peerPolicySide {
	side := peerPolicySide{darts: newDarts(b.keys), prefix: b.prefix, exact: b.exact, loose: b.loose, any: b.any}

	for _, index := range b.any {
		side.anySet.add(uint32(index))
	}

	return side
}

type PeerPolicies struct {
	rules []PeerPolicyRule
	from  peerPolicySide
	to    peerPolicySide
}

func (pm *IncludeParserManager) peerPolicies(value string) *PeerPolicies {
	paths := strings.Fields(value)

	if len(paths) == 0 {
		return nil
	}

	key := internStr(value)

	if policies, ok := pm.cache.peerPolicies.get(key); ok {
		return policies
	}

	policies := &PeerPolicies{}
	patterns := map[string]PeerPolicyPattern{}
	from := peerPolicySideBuilder{index: map[string]int{}}
	to := peerPolicySideBuilder{index: map[string]int{}}

	compile := func(pattern string) PeerPolicyPattern {
		if compiled, ok := patterns[pattern]; ok {
			return compiled
		}

		compiled := peerPolicyPattern(pattern)

		patterns[pattern] = compiled

		return compiled
	}

	for _, file := range paths {
		for line, text := range strings.Split(string(concatChunks(pm.fs.read(file))), "\n") {
			text = strings.TrimSpace(text)

			if text == "" || strings.HasPrefix(text, "#") {
				continue
			}

			fields := strings.Fields(text)
			policy := "DENY"

			if len(fields) == 4 {
				policy, fields = fields[0], fields[1:]
			}

			if len(fields) != 3 || fields[1] != "->" || (policy != "ALLOW" && policy != "DENY" && policy != "DEFAULT") {
				throwFmt("peerdir policy: %s:%d: invalid rule %s", file, line+1, text)
			}

			rule := PeerPolicyRule{from: compile(fields[0]), to: compile(fields[2]), allow: policy == "ALLOW"}

			from.place(len(policies.rules), rule.from)
			to.place(len(policies.rules), rule.to)
			policies.rules = append(policies.rules, rule)
		}
	}

	policies.from = from.side()
	policies.to = to.side()
	pm.cache.peerPolicies.put(key, policies)

	return policies
}

func (side *peerPolicySide) matching(rules []PeerPolicyRule, dir STR, self func(PeerPolicyRule) PeerPolicyPattern) []int {
	if matches, ok := side.matches.get(dir); ok {
		return matches
	}

	path := dir.string()

	var matches []int

	side.darts.walk(path, func(key int, whole bool) {
		for _, index := range side.prefix[key] {
			if re := self(rules[index]).re; re == nil || re.MatchString(path) {
				matches = append(matches, index)
			}
		}

		if whole {
			matches = append(matches, side.exact[key]...)
		}
	})

	for _, index := range side.loose {
		if self(rules[index]).matches(path) {
			matches = append(matches, index)
		}
	}

	slices.Sort(matches)
	matches = slices.Compact(matches)
	side.matches.put(dir, matches)

	return matches
}

func ruleFrom(rule PeerPolicyRule) PeerPolicyPattern {
	return rule.from
}

func ruleTo(rule PeerPolicyRule) PeerPolicyPattern {
	return rule.to
}

func (p *PeerPolicies) allows(from, to STR) bool {
	if p == nil {
		return true
	}

	froms := p.from.matching(p.rules, from, ruleFrom)
	tos := p.to.matching(p.rules, to, ruleTo)
	anyTo := p.to.any

	for len(tos) > 0 || len(anyTo) > 0 {
		var index int

		if len(anyTo) == 0 || len(tos) > 0 && tos[0] < anyTo[0] {
			index, tos = tos[0], tos[1:]
		} else {
			index, anyTo = anyTo[0], anyTo[1:]
		}

		if p.from.anySet.has(uint32(index)) {
			return p.rules[index].allow
		}

		if _, ok := slices.BinarySearch(froms, index); ok {
			return p.rules[index].allow
		}
	}

	return true
}

func (p *Platform) allowPeer(from, to STR, onWarn func(Warn)) bool {
	if p.peerPolicies.allows(from, to) {
		return true
	}

	onWarn(Warn{Kind: WarnModuleFailed, Message: fmt.Sprintf("PEERDIR from %s to %s is prohibited by peerdir policy and skipped", from.string(), to.string())})

	return false
}
