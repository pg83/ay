package main

func (d *ModuleData) recalcCpuCheck(env Environment) {
	value := d.cpuCheckBase

	if (!env.bool(envARCH_X86_64) && !env.bool(envARCH_I386)) || env.string(envDISABLE_INSTRUCTION_SETS) == "yes" || env.string(envUSE_SSE4) != "yes" || d.flags.NoUtil || env.string(envNoutil) == "yes" || env.string(envALLOCATOR) == "FAKE" {
		value = strNo
	}

	env.setStringID(envCpuCheck, value)
}

func (d *ModuleData) captureCpuCheckVariable(name ENV, env Environment) {
	if !d.cpuCheckBound {
		return
	}

	switch name {
	case envCpuCheck:
		d.cpuCheckBase = internStr(env.string(envCpuCheck))
	case envNoutil:
		d.flags.NoUtil = env.string(name) == "yes"
		d.recalcCpuCheck(env)
	case envUSE_SSE4, envALLOCATOR, envDISABLE_INSTRUCTION_SETS:
		d.recalcCpuCheck(env)
	}
}

func (d *ModuleData) captureCpuCheckMacro(v UnknownStmt, env Environment) {
	if !d.cpuCheckBound {
		return
	}

	switch v.Name {
	case tokEnable, tokDisable, tokSetAppend:
		for i, arg := range v.Args {
			if v.Name == tokSetAppend && i > 0 {
				break
			}

			d.captureCpuCheckVariable(internEnv(arg.string()), env)
		}
	case tokAllocator:
		d.recalcCpuCheck(env)
	case tokNoUtil, tokNoRuntime, tokNoLibc, tokNoPlatform:
		env.setStringID(envNoutil, strYes)
		d.recalcCpuCheck(env)
	}
}
