package main

import (
	"regexp"
	"slices"
	"strings"
)

var (
	mlaunchQualifiedName = regexp.MustCompile(`^[A-Za-z_]\w*(::[A-Za-z_]\w*)*$`)
	mlaunchName          = regexp.MustCompile(`^[A-Za-z_]\w*$`)
)

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

func mlaunchBuilderOptions(macro string, args []string, allowed []string, report func(string)) (string, map[string]string, bool) {
	symbol := ""

	if len(args) > 0 {
		symbol, args = args[0], args[1:]
	}

	if len(args)%2 != 0 {
		report(macro + " expects keyword/value pairs")

		return "", nil, false
	}

	options := map[string]string{}

	for i := 0; i < len(args); i += 2 {
		key := args[i]

		if _, seen := options[key]; seen || !slices.Contains(allowed, key) {
			report("Unknown or repeated " + macro + " option: " + key)

			return "", nil, false
		}

		options[key] = args[i+1]
	}

	return symbol, options, true
}

func nodeBuilderValid(args []string, env Environment, report func(string)) bool {
	symbol, options, ok := mlaunchBuilderOptions("NODE_BUILDER", args, []string{"NAME", "TARGET", "HEADER", "ROS_SPINNER_THREADS"}, report)

	if !ok {
		return false
	}

	var missing []string

	for _, key := range [...]string{"HEADER", "NAME", "TARGET"} {
		if _, ok := options[key]; !ok {
			missing = append(missing, key)
		}
	}

	spinner, ok := options["ROS_SPINNER_THREADS"]

	if !ok {
		spinner = "0"
	}

	switch {
	case len(missing) > 0:
		report("Missing NODE_BUILDER options: " + strings.Join(missing, ", "))
	case env.string(envMlaunchNodeDeclared) == "yes":
		report("MLAUNCH_COMPONENT supports one NODE_BUILDER; compose multiple leaf components instead")
	case !mlaunchQualifiedName.MatchString(symbol):
		report("NODE_BUILDER requires a qualified C++ function name")
	case spinner == "" || strings.Trim(spinner, "0123456789") != "":
		report("ROS_SPINNER_THREADS must be a non-negative integer")
	case !mlaunchName.MatchString(options["NAME"]):
		report("Invalid NODE_BUILDER NAME")
	default:
		env.setString(envMlaunchNodeDeclared, "yes")

		return true
	}

	return false
}

func compoundNodeBuilderValid(args []string, env Environment, report func(string)) bool {
	symbol, options, ok := mlaunchBuilderOptions("COMPOUND_NODE_BUILDER", args, []string{"TARGET", "HEADER", "NAME"}, report)

	if !ok {
		return false
	}

	keys := strings.Fields(env.string(envMlaunchCompoundKeys))
	key := options["NAME"]

	switch {
	case len(options) != 3:
		report("COMPOUND_NODE_BUILDER requires TARGET, HEADER and NAME")
	case !mlaunchQualifiedName.MatchString(symbol):
		report("COMPOUND_NODE_BUILDER requires a qualified C++ function name")
	case !mlaunchName.MatchString(key) || slices.Contains(keys, key):
		report("Invalid or repeated compound builder NAME: " + key)
	default:
		env.setString(envMlaunchCompoundKeys, strings.Join(append(keys, key), " "))

		return true
	}

	return false
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
