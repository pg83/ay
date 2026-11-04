package main

import "fmt"

func macroLimit(d *ModuleData, name TOK) (ignored, restricted bool) {
	if d.moduleStmt == nil || d.confModule == nil {
		return false, false
	}

	limits, macro := d.confModule.limits, name.str()

	if to, ok := d.confModule.renames[macro]; ok {
		macro = to
	}

	return limits.ignored.has(macro), limits.restricted.has(macro) && !limits.allowed.has(macro)
}

func policyDrops(d *ModuleData, name TOK) bool {
	ignored, restricted := macroLimit(d, name)

	return ignored || restricted
}

func dropsMacro(d *ModuleData, modulePath string, name TOK, onWarn func(Warn)) bool {
	ignored, restricted := macroLimit(d, name)

	if !ignored && restricted {
		onWarn(Warn{Kind: WarnMisconfiguration, Message: fmt.Sprintf("%s: %s is not allowed for %s modules", modulePath, name.string(), d.moduleStmt.Name.string())})
	}

	return ignored || restricted
}
