package main

import (
	"fmt"
	"slices"
)

var (
	pyAnyModuleMacros = MacroPolicy{restricted: []TOK{tokUsePython2, tokUsePython3, tokPySrcs, tokPyMain}}
	py2ModuleMacros   = MacroPolicy{restricted: append(slices.Clip(pyAnyModuleMacros.restricted), tokPython3Module, tokPython3Addincl)}
	py3ModuleMacros   = MacroPolicy{restricted: append(slices.Clip(pyAnyModuleMacros.restricted), tokPython2Module, tokPython2Addincl)}
	py2LibraryMacros  = MacroPolicy{restricted: []TOK{tokPython3Addincl, tokUsePython3, tokPython2Addincl}, ignored: []TOK{tokUsePython2}}
	py3LibraryMacros  = MacroPolicy{restricted: []TOK{tokPython3Addincl, tokUsePython3, tokPython2Addincl, tokUsePython2}}
	py23LibraryMacros = uniteMacroPolicies(py2LibraryMacros, py3LibraryMacros)
	py23TestMacros    = uniteMacroPolicies(py2LibraryMacros, py3LibraryMacros, py3LibraryMacros)
)

var py23NativeLibraryMacros = uniteMacroPolicies(
	MacroPolicy{restricted: []TOK{tokPySrcs, tokUsePython2, tokUsePython3, tokPython3Addincl}},
	MacroPolicy{restricted: []TOK{tokPySrcs, tokUsePython2, tokUsePython3}},
)

type MacroPolicy struct {
	restricted []TOK
	ignored    []TOK
}

func uniteMacroPolicies(variants ...MacroPolicy) []MacroPolicy {
	out := make([]MacroPolicy, len(variants))

	for i, v := range variants {
		out[i].ignored = slices.Clone(v.ignored)

		for _, name := range v.restricted {
			everywhere := true

			for _, other := range variants {
				everywhere = everywhere && slices.Contains(other.restricted, name)
			}

			if everywhere {
				out[i].restricted = append(out[i].restricted, name)
			} else {
				out[i].ignored = append(out[i].ignored, name)
			}
		}
	}

	return out
}

func moduleMacroPolicy(d *ModuleData) MacroPolicy {
	switch d.moduleStmt.Name {
	case tokPyAnyModule:
		return pyAnyModuleMacros
	case tokPy2Module:
		return py2ModuleMacros
	case tokPy3Module:
		return py3ModuleMacros
	case tokPy2Library, tokPy2Program, tokPy2Test, tokPytestBin:
		return py2LibraryMacros
	case tokPy3Library, tokPy3Program, tokPy3ProgramBin, tokPy3Test, tokPy3TestBin:
		return py3LibraryMacros
	case tokPy23Library:
		return python2Variant(py23LibraryMacros, d.unit.Type == tokPy2Library)
	case tokPy23NativeLibrary:
		return python2Variant(py23NativeLibraryMacros, d.unit.Type == tokLibrary)
	case tokPy23Test:
		return python2Variant(py23TestMacros, d.unit.Type == tokPytestBin)
	}

	return MacroPolicy{}
}

func python2Variant(variants []MacroPolicy, python2 bool) MacroPolicy {
	if python2 {
		return variants[0]
	}

	return variants[1]
}

func dropsMacro(d *ModuleData, modulePath string, name TOK, onWarn func(Warn)) bool {
	if d.moduleStmt == nil {
		return false
	}

	policy := moduleMacroPolicy(d)

	if slices.Contains(policy.ignored, name) {
		return true
	}

	if !slices.Contains(policy.restricted, name) {
		return false
	}

	onWarn(Warn{Kind: WarnMisconfiguration, Message: fmt.Sprintf("%s: %s is not allowed for %s modules", modulePath, name.string(), d.moduleStmt.Name.string())})

	return true
}
