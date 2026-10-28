package main

import (
	"maps"
	"slices"
	"strings"
)

var (
	confTargetArena        = newBumpAllocator[ConfTarget]()
	confModuleArena        = newBumpAllocator[ConfModule]()
	confModulePointerArena = newBumpAllocator[*ConfModule]()
)

type ConfValue struct {
	value STR
	kind  EnvKind
}

func confStringValue(value string) ConfValue {
	if vfsHasPrefix(value) {
		return ConfValue{value: STR(intern(value)), kind: envVFS}
	}

	return ConfValue{value: internStr(value), kind: envStr}
}

func confStringValueID(value STR) ConfValue {
	if vfsHasPrefix(value.string()) {
		return ConfValue{value: STR(intern(value.string())), kind: envVFS}
	}

	return ConfValue{value: value, kind: envStr}
}

func confEnvValue(env Environment, key ENV) ConfValue {
	kind, value := env.s.lookup(key)

	if kind == envAbsent {
		value = 0
	}

	return ConfValue{value: value, kind: kind}
}

func confStringBinding(env Environment, key ENV) ConfValue {
	v := confEnvValue(env, key)

	if v.kind != envAbsent && v.kind != envVFS {
		v = confStringValueID(v.value)
	}

	return v
}

func (v ConfValue) apply(env Environment, key ENV) {
	switch v.kind {
	case envAbsent:
		env.unbind(key)
	case envVFS:
		env.setVFS(key, VFS(v.value))
	default:
		env.setStringID(key, v.value)
		env.s.kind[key] = v.kind
	}
}

type ConfConditions struct {
	source      []ConfStmtRef
	byVariable  DenseMap[ENV, []ENV]
	byTarget    DenseMap[ENV, ConfTarget]
	modules     DenseMap[STR, *ConfModule]
	variants    DenseMap[STR, []ConfVariant]
	values      *BumpAllocator[ConfValue]
	assignments *BumpAllocator[ConfAssignment]
}

type ConfTarget struct {
	stmts     []ConfStmtRef
	inherited ConfValue
}

type ConfVariant struct {
	name  STR
	value string
}

type ConfModule struct {
	parent   STR
	variable ENV
	builder  []ENV
	init     *ConfModuleInit
}

type confDependency struct{ variable, target ENV }

func confConditionInput(r ConfStmtRef) bool {
	return r.stmt.branch != 0 || len(r.file.modules) != 0 || len(r.file.variants) != 0 || len(r.file.calls) != 0
}

func (c *ConfConditions) matches(stmts []ConfStmtRef) bool {
	i := 0

	for _, r := range stmts {
		if confConditionInput(r) {
			if i == len(c.source) || c.source[i] != r {
				return false
			}

			i++
		}
	}

	return i == len(c.source)
}

func newConfConditions(stmts []ConfStmtRef) *ConfConditions {
	c := &ConfConditions{}

	for _, r := range stmts {
		if confConditionInput(r) {
			c.source = append(c.source, r)
		}
	}

	stmts = c.source

	seen := map[*ConfFile]bool{}
	calls := map[string][]string{}

	var counts DenseMap[ENV, int]
	var dependencies []confDependency

	conditionalCount := 0

	for _, r := range stmts {
		if !seen[r.file] {
			for name, parent := range r.file.modules {
				c.modules.put(internStr(name), &ConfModule{parent: internStr(parent), variable: internEnvSTR(internV("env", name))})
			}

			for name, variants := range r.file.variants {
				ids := make([]ConfVariant, len(variants))

				for i, variant := range variants {
					ids[i] = ConfVariant{name: internStr(variant), value: variant}
				}

				c.variants.put(internStr(name), ids)
			}

			maps.Copy(calls, r.file.calls)
			seen[r.file] = true
		}

		if r.stmt.branch == 0 {
			continue
		}

		conditionalCount++

		target := r.stmt.name
		count, _ := counts.get(target)

		counts.put(target, count+1)

		r.file.condVars(r.stmt.branch, func(variable ENV) {
			dependencies = append(dependencies, confDependency{variable, target})
		})
	}

	slices.SortFunc(dependencies, func(a, b confDependency) int {
		if a.variable != b.variable {
			return int(a.variable) - int(b.variable)
		}

		return strings.Compare(a.target.string(), b.target.string())
	})

	dependencies = slices.Compact(dependencies)

	targets := make([]ENV, len(dependencies))

	for i := 0; i < len(dependencies); {
		start, variable := i, dependencies[i].variable

		for i < len(dependencies) && dependencies[i].variable == variable {
			targets[i] = dependencies[i].target
			i++
		}

		c.byVariable.put(variable, targets[start:i:i])
	}

	refs := make([]ConfStmtRef, conditionalCount)

	for key, slot := range counts.idx.each() {
		n := counts.vals.s[slot]

		c.byTarget.put(ENV(key), ConfTarget{stmts: refs[:0:n]})
		refs = refs[n:]
	}

	for _, r := range stmts {
		if r.stmt.branch != 0 {
			target, _ := c.byTarget.get(r.stmt.name)

			target.stmts = append(target.stmts, r)
			c.byTarget.put(r.stmt.name, target)
		}
	}

	for _, module := range c.modules.vals.s {
		if module == nil {
			continue
		}

		module.builder = c.builderMacros(calls, module.variable.string(), map[string]bool{}, nil)
	}

	return c
}

func (c *ConfConditions) bind(env Environment) *ConfConditions {
	b := *c

	b.values = newBumpAllocator[ConfValue]()
	b.assignments = newBumpAllocator[ConfAssignment]()

	targets := confTargetArena.alloc(len(c.byTarget.vals.s))

	copy(targets, c.byTarget.vals.s)

	for key, slot := range c.byTarget.idx.each() {
		targets[slot].inherited = confStringBinding(env, ENV(key))
	}

	confTargetArena.commit(len(c.byTarget.vals.s))
	b.byTarget.vals.s = targets[:len(c.byTarget.vals.s):len(c.byTarget.vals.s)]

	n := len(c.modules.vals.s)
	modules := confModuleArena.alloc(n)
	pointers := confModulePointerArena.alloc(n)

	for i, module := range c.modules.vals.s {
		if module != nil {
			modules[i] = *module
			pointers[i] = &modules[i]
		}
	}

	confModuleArena.commit(n)
	confModulePointerArena.commit(n)
	b.modules.vals.s = pointers[:n:n]

	return &b
}

func (c *ConfConditions) builderMacros(calls map[string][]string, guard string, seen map[string]bool, out []ENV) []ENV {
	for _, call := range calls[guard] {
		if seen[call] {
			continue
		}

		seen[call] = true

		variable := internEnv(call)

		if targets, _ := c.byVariable.get(variable); len(targets) > 0 {
			out = append(out, variable)
		}

		out = c.builderMacros(calls, call, seen, out)
	}

	return out
}

func (p *Platform) recalcConf(env Environment, variable ENV, originals map[STR]STR) {
	if p.conditions != nil {
		p.recalcConfInto(env, variable, originals, nil)
	}
}

func (p *Platform) recalcConfInto(env Environment, variable ENV, originals map[STR]STR, recording []ConfAssignment) {
	c := p.conditions
	targets, _ := c.byVariable.get(variable)

	for _, target := range targets {
		env.unbind(target)

		definition, _ := c.byTarget.get(target)
		base := definition.inherited

		if original, ok := originals[target.str()]; ok {
			if inherited := p.ifEnv.string(target); base.kind != envAbsent && inherited != "" {
				confStringValueID(internV(inherited, " ", original.string())).apply(env, target)
			} else {
				env.setStringID(target, original)
			}
		} else if base.kind != envAbsent {
			base.apply(env, target)
		}

		for _, r := range definition.stmts {
			if r.file.holds(r.stmt.branch, env) {
				applyConfStmt(env, *r.stmt, true)
			}
		}

		if recording != nil {
			recordConfAssignment(recording, target, env)
		}
	}
}

func (c *ConfConditions) module(name STR, tag string) *ConfModule {
	if variants, _ := c.variants.get(name); len(variants) > 0 {
		for _, variant := range variants {
			if strings.HasPrefix(variant.value, tag) && strings.HasPrefix(variant.value[len(tag):], "__from__") {
				name = variant.name

				break
			}
		}
	}

	module, _ := c.modules.get(name)

	return module
}

func (p *Platform) finishConfModule(env Environment, name STR, tag string, originals map[STR]STR) {
	if p.conditions == nil {
		return
	}

	module := p.conditions.module(name, tag)

	if module == nil {
		return
	}

	p.finishConfModuleParent(env, module, originals)
}

func (p *Platform) finishConfModuleParent(env Environment, module *ConfModule, originals map[STR]STR) {
	c := p.conditions

	if parent := c.module(module.parent, ""); parent != nil {
		p.finishConfModuleParent(env, parent, originals)
	}

	for _, variable := range module.builder {
		env.setBool(variable, true)

		targets, _ := c.byVariable.get(variable)

		if slices.ContainsFunc(targets, func(target ENV) bool { return originals[target.str()] != 0 }) {
			p.recalcConf(env, variable, originals)
		}
	}
}
