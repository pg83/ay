package main

import (
	"crypto/md5"
	enc32 "encoding/base32"
	"strings"
)

const importMaskVariable = "NO_CHECK_IMPORTS_FOR_VALUE"

// TVars keeps the raw local expression and its inherited BaseVal separately.
// ResetAppend introduces a self reference to that base; it is not a flattened
// list. Keep this source binding separate from the checker metadata list.
type pythonImportMask struct {
	base        string
	local       bool
	original    string
	hasOriginal bool
	resource    *ResourceItem
}

func initPythonImportMask(env Environment, d *ModuleData) {
	key := internEnv(importMaskVariable)
	env.setDefaultString(key, "None") // ymake.core.conf's inherited default
	d.importMask.base = env.string(key)
}

func setupPythonImportMask(env Environment, d *ModuleData) {
	key := internEnv(importMaskVariable)
	if isPyProgramType(d.unit.Type) {
		// RecalcVars restores source originals with ResetAppend when the program
		// configuration initializes its conditional assignment to this variable.
		if d.importMask.local {
			if d.importMask.hasOriginal {
				env.setString(key, "$"+importMaskVariable+" "+d.importMask.original)
			} else {
				env.setString(key, d.importMask.base)
				d.importMask.local = false
			}
		}
		if env.bool(envSANITIZER_TYPE) && env.string(envSANITIZER_TYPE) != "no" {
			env.setString(key, "")
			d.importMask.local = true
		}
	}
}

func setPythonImportMask(value string, d *ModuleData) {
	d.importMask.local = true
	d.importMask.original = value
	d.importMask.hasOriginal = true
}

func appendPythonImportMask(value string, env Environment, d *ModuleData) string {
	previous := env.string(internEnv(importMaskVariable))
	if !d.importMask.local || previous == "" {
		previous = "$" + importMaskVariable
	}
	if d.importMask.original != "" {
		d.importMask.original += " "
	}
	d.importMask.original += value
	d.importMask.hasOriginal = true
	d.importMask.local = true
	return previous + " " + value
}

// Reserved roots/resources remain expressions during source macro binding
// and GetSubst. Their concrete bindings arrive with the final command owner.
func pythonImportMaskSourceEnv(env Environment) Environment {
	locals := env.clone()
	for i, kind := range locals.s.kind {
		if kind != envAbsent && runProgramReservedVariable(ENV(i).string()) {
			locals.unbind(ENV(i))
		}
	}
	return locals
}

// _REGISTER_NO_CHECK_IMPORTS hashes unit.get (raw), while NoCheck metadata
// uses unit.get_subst. The RESOURCE command is compiled only after those
// source/configuration bindings have been made.
func finishPythonImportMask(env Environment, d *ModuleData) {
	raw := env.string(internEnv(importMaskVariable))
	d.noCheckImportsDisabled = strings.TrimSpace(raw) == ""
	if raw == "" || raw == "None" {
		return
	}
	base := pythonImportMaskSourceEnv(env)
	base.setString(internEnv(importMaskVariable), d.importMask.base)
	expanded := expandConfigMacroBody(raw, base)
	for _, mask := range strings.Fields(strings.ReplaceAll(expanded, `${"$"}`, "$")) {
		if mask != `""` && mask != `''` {
			d.noCheckImports = append(d.noCheckImports, internStr(mask).any())
		}
	}
	if len(d.noCheckImports) == 0 {
		d.noCheckImports = append(d.noCheckImports, internStr("*").any())
	}
	sum := md5.Sum([]byte(raw))
	id := strings.ToLower(strings.TrimRight(enc32.StdEncoding.EncodeToString(sum[:]), "="))
	kv := "py/no_check_imports/" + id + "=\"" + raw + "\""
	d.importMask.resource = &ResourceItem{Path: "-", Key: kv}
}

// TCommands::TInliner follows BaseVal for recursive references. A multiargument
// definition in a scalar term stays deferred at depth zero. CollectVarsDeep
// then compiles the raw definition as a command in its own right, and the
// runtime evaluator concatenates its argument vector without separators.
func legacyConfigurationCommand(text string, env Environment, variable, value, base string) []string {
	depths := map[string]int{}
	var compile func(string, bool, int) []string
	compile = func(text string, scalar bool, depth int) []string {
		if depth > 50 { // TInliner::CheckDepth
			throwFmt("configuration command expansion is too deep")
		}
		args, _ := commandArgumentExpression(text, func(name string, term bool) ([]string, string) {
			definition, _ := env.lookup(name)
			if name == variable {
				definition = value
			}
			if depths[name] != 0 {
				if name != variable || depths[name] != 1 {
					throwFmt("self-contradictory variable definition (%s)", name)
				}
				definition = base
			}
			if scalar || term {
				shape, _ := commandArgumentExpression(definition, func(string, bool) ([]string, string) { return []string{"variable"}, "" })
				if len(shape) > 1 {
					if depths[name] != 0 {
						throwFmt("unexpected multiargument substitution: %s", name)
					}
					outer := depths
					depths = map[string]int{}
					values := compile(definition, false, depth+1)
					depths = outer
					return values, ""
				}
			}
			depths[name]++
			values := compile(definition, scalar || term, depth+1)
			depths[name]--
			return values, ""
		})
		return args
	}
	return compile(text, false, 0)
}
