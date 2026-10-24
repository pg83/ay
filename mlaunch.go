package main

import "strings"

type MlaunchModule struct {
	Generate   string
	Args       []string
	Functions  []string
	Suffix     string
	Components []ANY
}

func mlaunchModuleUnit(name TOK, language Language) ModuleUnit {
	base := tokLibrary
	tag := "CPP_MLAUNCH"

	if name == tokGolaunchProgram || name == tokGolaunchComposite {
		base = tokGoProgram
		tag = ""
	}

	if name == tokMlaunchPipeline {
		base = tokProgram
		tag = "PIPELINE"

		if language == LangGo {
			base = tokGoProgram
			tag = "GOLAUNCH"
		}

		if language == LangPackage {
			base = tokPackageFinal
			tag = "DEPLOY"
		}
	} else if name == tokMlaunchLibrary || name == tokMlaunchComponent {
		if language == LangGo {
			base = tokGoLibrary
			tag = "GO"
		}

		if language == LangPackage {
			base = tokUnion
			tag = "PACKAGE_UNION"
		}
	}

	unit := resolveModuleUnitBare(base, KindLib, language)

	unit.Tag = internStr(tag)
	unit.CCTag = internStr(strings.ToLower(tag))
	unit.GlobalARTag = internStr(strings.ToLower(tag) + "_global")

	return unit
}

func setupMlaunchModule(stmt *ModuleStmt, module string, language Language, env Environment, d *ModuleData) {
	switch stmt.Name {
	case tokMlaunchLibrary, tokMlaunchComponent, tokMlaunchPipeline, tokGolaunchProgram, tokGolaunchComposite:
	default:
		return
	}

	d.multimodule = stmt.Name
	d.mlaunch = &MlaunchModule{}
	env.setStringID(envMODULE_TAG, d.unit.Tag)

	peer := func(paths ...string) { d.peerdirs = append(d.peerdirs, internAnys(paths)...) }

	switch stmt.Name {
	case tokMlaunchLibrary:
		d.moduleVariants = 1<<LangCPP | 1<<LangGo | 1<<LangPackage

		if language == LangGo {
			peer("sdg/sdc/ros/configuration/golaunch", "sdg/sdc/ros/configuration/golaunch/golaunch_context")
		}
	case tokMlaunchComponent:
		if language == LangGo {
			peer("sdg/sdc/ros/configuration/golaunch", "sdg/sdc/ros/configuration/golaunch/golaunch_context")
		}
	case tokGolaunchProgram:
		d.mlaunch.Generate = "golaunch"
		peer("sdg/sdc/ros/configuration/golaunch")
	case tokGolaunchComposite:
		d.mlaunch.Generate = "composite"

		std := env.string(envGOSTD)

		peer(std+"/fmt", std+"/sort", "sdg/sdc/ros/configuration/golaunch")
	case tokMlaunchPipeline:
		d.moduleVariants = 1<<LangCPP | 1<<LangGo | 1<<LangPackage
		applyUnknownStmt(nil, module, UnknownStmt{Name: tokUseSdcMeta}, d, env, nil)

		switch language {
		case LangGo:
			d.mlaunch.Generate = "golaunch"
			d.mlaunch.Suffix = ".golaunch"
			peer("sdg/sdc/ros/configuration/golaunch")

			name := programBinaryName(ModuleInstance{Path: source(module)}, stmt) + ".golaunch"
			renamed := *d.moduleStmt

			renamed.Args = []ANY{internStr(name).any()}
			d.moduleStmt = &renamed
		case LangPackage:
			d.mlaunch.Suffix = ".deploy"
			d.selfPeers = append(d.selfPeers, LangCPP, LangGo)
			peer("sdg/sdc/launcher/profiles/sdc_install", "sdg/sdc/ros/vehicles/sdc_install")
		default:
			peer("sdg/sdc/launcher/myriapodalaunch/yamakes/myriapodalaunch_main")

			d.ldFlags = append(d.ldFlags, strWlRpathOrigin.any())
			env.setBool(envUseDynamicCuda, true)
		}
	}
}

func applyMlaunchStmt(module string, stmt UnknownStmt, env Environment, d *ModuleData) {
	args := anyStrs(stmt.Args)
	goModule := isGoModuleType(d.unit.Type)

	switch stmt.Name {
	case tokRuntimePeerdir:
		if d.multimodule == tokMlaunchLibrary && !goModule && d.unit.Type != tokUnion {
			d.peerdirs = append(d.peerdirs, stmt.Args...)
		}
	case tokResourcePeerdir:
		if d.multimodule == tokMlaunchLibrary && d.unit.Type == tokUnion {
			d.peerdirs = append(d.peerdirs, stmt.Args...)
		}
	case tokGolaunchSrcdir:
		if goModule {
			for _, dir := range args {
				d.srcDirs = append(d.srcDirs, source(dir))
			}
		}
	case tokGolaunchSrcs:
		if goModule {
			for _, file := range stmt.Args {
				d.srcs = append(d.srcs, SrcMeta{Source: file, Prio: stmtPrioSrcs, Seq: d.nextDeclSeq()})
			}
		}
	case tokNoGolaunchSrcs:
		if goModule {
			ensureMlaunch(d).Generate = "component"
			ensureMlaunch(d).Functions = append(ensureMlaunch(d).Functions, module+":Launch")
		}
	case tokGolaunchFunction:
		if len(args) != 1 {
			throwFmt("GOLAUNCH_FUNCTION expects one function")
		}

		if goModule {
			ensureMlaunch(d).Functions = append(ensureMlaunch(d).Functions, module+":"+args[0])
		}
	case tokGolaunchOverride:
		if len(args) != 2 {
			throwFmt("GOLAUNCH_OVERRIDE expects a target and function")
		}

		ensureMlaunch(d).Args = append(ensureMlaunch(d).Args, "--deps-override", strings.Join(args, ":"))
	case tokGolaunchCompositeComps:
		ensureMlaunch(d).Components = append(ensureMlaunch(d).Components, stmt.Args...)
		d.peerdirs = append(d.peerdirs, stmt.Args...)
	case tokGenerateGolaunchCallbacks:
		if len(args) != 1 {
			throwFmt("GENERATE_GOLAUNCH_CALLBACKS expects one name")
		}

		ensureMlaunch(d).Args = append(ensureMlaunch(d).Args, "--gen-callbacks", args[0])
	}
}

func checkNodeBuilderOptions(args []ANY) {
	if len(args)%2 != 1 {
		throwFmt("NODE_BUILDER expects a builder symbol and keyword/value pairs")
	}

	seen := map[string]bool{}

	for i := 1; i < len(args); i += 2 {
		key := args[i].string()

		switch key {
		case "NAME", "TARGET", "HEADER", "ROS_SPINNER_THREADS":
		default:
			throwFmt("Unknown NODE_BUILDER option: %s", key)
		}

		if seen[key] {
			throwFmt("Repeated NODE_BUILDER option: %s", key)
		}

		seen[key] = true
	}

	if !seen["NAME"] || !seen["TARGET"] || !seen["HEADER"] {
		throwFmt("NODE_BUILDER requires NAME, TARGET and HEADER")
	}
}

func ensureMlaunch(d *ModuleData) *MlaunchModule {
	if d.mlaunch == nil {
		d.mlaunch = &MlaunchModule{}
	}

	return d.mlaunch
}

func mlaunchDeps(d *ModuleData, resolved []ResolvedPeer) []string {
	var result []string

	for _, peer := range resolved {
		result = append(result, peer.result.MlaunchDeps...)
	}

	if d.mlaunch != nil {
		result = append(result, d.mlaunch.Functions...)
	}

	return dedupKeepOrder(result)
}
