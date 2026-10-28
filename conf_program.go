package main

import (
	"slices"
	"strings"
)

type ConfProgram struct {
	stmts      []ConfStmtRef
	macros     map[STR]*ConfMacro
	conditions *ConfConditions
}

func (pm *IncludeParserManager) confProgram(f *ConfFile, walking []string) *ConfProgram {
	if f.program != nil {
		return f.program
	}

	p := &ConfProgram{macros: map[STR]*ConfMacro{}}

	for name, macro := range f.macros {
		p.macros[name] = macro
	}

	for i := range f.stmts {
		s := &f.stmts[i]

		if s.name != 0 {
			p.stmts = append(p.stmts, ConfStmtRef{file: f, stmt: s})

			continue
		}

		if slices.Contains(walking, s.value) {
			throwFmt("conf: @import loop: %s -> %s", strings.Join(walking, " -> "), s.value)
		}

		imported := pm.confProgram(pm.confFile(s.value), append(walking, s.value))

		p.stmts = append(p.stmts, imported.stmts...)

		for name, macro := range imported.macros {
			if previous, ok := p.macros[name]; !ok || previous != nil {
				p.macros[name] = macro
			}
		}
	}

	f.program = p

	return p
}

func (p *ConfProgram) conditionsFor(pm *IncludeParserManager) *ConfConditions {
	if p.conditions != nil {
		return p.conditions
	}

	for _, conditions := range pm.cache.confConditions {
		if conditions.matches(p.stmts) {
			p.conditions = conditions

			return conditions
		}
	}

	p.conditions = newConfConditions(p.stmts)
	pm.cache.confConditions = append(pm.cache.confConditions, p.conditions)

	return p.conditions
}
