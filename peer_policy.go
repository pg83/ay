package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

type PeerPolicyPattern struct {
	prefix string
	re     *regexp.Regexp
	exact  bool
}

func peerPolicyPattern(pattern string) PeerPolicyPattern {
	pattern = strings.TrimPrefix(pattern, "^")

	if pattern == ".*" {
		return PeerPolicyPattern{}
	}

	literal := true
	value, exact := strings.CutSuffix(pattern, "$")

	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("/-_", c) || c == '.' && !exact) {
			literal = false

			break
		}
	}

	if literal {
		return PeerPolicyPattern{prefix: value, exact: exact}
	}

	re := throw2(regexp.Compile("^" + pattern))
	prefix, _ := re.LiteralPrefix()

	return PeerPolicyPattern{prefix: prefix, re: re}
}

func (p PeerPolicyPattern) matches(dir string) bool {
	if p.exact {
		return dir == p.prefix
	}

	return strings.HasPrefix(dir, p.prefix) && (p.re == nil || p.re.MatchString(dir))
}

type PeerPolicyRule struct {
	from  PeerPolicyPattern
	to    PeerPolicyPattern
	allow bool
}

type PeerPolicies struct {
	rules       []PeerPolicyRule
	fromRoots   map[string][]int
	fromMatches DenseMap[STR, []int]
}

func (pm *IncludeParserManager) peerPolicies(paths []string) *PeerPolicies {
	if len(paths) == 0 {
		return nil
	}

	key := internStr(strings.Join(paths, "\n"))

	if policies, ok := pm.cache.peerPolicies.get(key); ok {
		return policies
	}

	policies := &PeerPolicies{fromRoots: map[string][]int{}}
	patterns := map[string]PeerPolicyPattern{}

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
			root, _, rooted := strings.Cut(rule.from.prefix, "/")

			if !rooted {
				root = ""
			}

			policies.fromRoots[root] = append(policies.fromRoots[root], len(policies.rules))
			policies.rules = append(policies.rules, rule)
		}
	}

	pm.cache.peerPolicies.put(key, policies)

	return policies
}

func (p *PeerPolicies) matchingFrom(from STR) []int {
	if matches, ok := p.fromMatches.get(from); ok {
		return matches
	}

	dir := from.string()
	root, _, _ := strings.Cut(dir, "/")

	var matches []int

	for _, group := range []string{"", root} {
		for _, index := range p.fromRoots[group] {
			if p.rules[index].from.matches(dir) {
				matches = append(matches, index)
			}
		}
	}

	slices.Sort(matches)
	p.fromMatches.put(from, matches)

	return matches
}

func (p *PeerPolicies) allows(from, to STR) bool {
	if p != nil {
		for _, index := range p.matchingFrom(from) {
			if rule := p.rules[index]; rule.to.matches(to.string()) {
				return rule.allow
			}
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
