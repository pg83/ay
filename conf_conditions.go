package main

import (
	"maps"
	"slices"
	"strings"
)

type ConfConditions struct {
	byVariable map[ENV][]ENV
	byTarget   map[ENV][]ConfStmtRef
	modules    map[string]ConfModule
	variants   map[string][]string
}

type ConfModule struct {
	parent   string
	variable ENV
}

func newConfConditions(stmts []ConfStmtRef) *ConfConditions {
	c := &ConfConditions{byVariable: map[ENV][]ENV{}, byTarget: map[ENV][]ConfStmtRef{}, modules: map[string]ConfModule{}, variants: map[string][]string{}}
	seen := map[*ConfFile]bool{}

	for _, r := range stmts {
		if !seen[r.file] {
			for name, parent := range r.file.modules {
				c.modules[name] = ConfModule{parent: parent, variable: internEnv("env" + name)}
			}

			maps.Copy(c.variants, r.file.variants)
			seen[r.file] = true
		}

		if r.stmt.branch == 0 {
			continue
		}

		target := internEnv(r.stmt.name)

		c.byTarget[target] = append(c.byTarget[target], r)

		r.file.condVars(r.stmt.branch, func(name string) {
			variable := internEnv(name)

			if !slices.Contains(c.byVariable[variable], target) {
				c.byVariable[variable] = append(c.byVariable[variable], target)
			}
		})
	}

	for _, targets := range c.byVariable {
		slices.SortFunc(targets, func(a, b ENV) int { return strings.Compare(a.string(), b.string()) })
	}

	return c
}

func (p *Platform) recalcConf(env Environment, variable ENV, originals map[STR]STR) {
	if p.conditions == nil {
		return
	}

	for _, target := range p.conditions.byVariable[variable] {
		env.unbind(target)

		base, inherited := p.ifEnv.lookup(target.string())

		if original, ok := originals[internStr(target.string())]; ok {
			if inherited && base != "" {
				env.setString(target, base+" "+original.string())
			} else {
				env.setStringID(target, original)
			}
		} else if inherited {
			env.setString(target, base)
		}

		for _, r := range p.conditions.byTarget[target] {
			if r.file.holds(r.stmt.branch, env) {
				applyConfStmt(env, r.stmt, true)
			}
		}
	}
}

func (p *Platform) initConfModule(env Environment, name, tag string, originals map[STR]STR) {
	if p.conditions == nil {
		return
	}

	if variants := p.conditions.variants[name]; len(variants) > 0 {
		for _, variant := range variants {
			if strings.HasPrefix(variant, tag+"__from__") {
				name = variant

				break
			}
		}
	}

	module, ok := p.conditions.modules[name]

	if !ok {
		return
	}

	if module.parent != "" {
		p.initConfModule(env, module.parent, "", originals)
	}

	env.setBool(module.variable, true)
	p.recalcConf(env, module.variable, originals)

	if name == "_BASE_UNIT" {
		env.setBool(envBisonFlexSetDefaults, true)
		p.recalcConf(env, envBisonFlexSetDefaults, originals)
	}

	if name == "_LINK_UNIT" || name == "GO_PROGRAM" {
		env.setString(envLinkerId, env.string(envDefaultLinkerId))
		p.recalcConf(env, envLinkerId, originals)

		common := envCommonLinkSettings

		env.setBool(common, true)
		p.recalcConf(env, common, originals)
	}
}
