package main

import "fmt"

func macroLimit(d *ModuleData, name TOK) (ignored, restricted bool) {
	if d.moduleStmt == nil || d.confModule == nil {
		return false, false
	}

	verdict := d.confModule.verdicts[name.str()]

	return verdict&confMacroIgnored != 0, verdict&confMacroRestricted != 0
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
