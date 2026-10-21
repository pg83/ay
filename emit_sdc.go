package main

import (
	"path"
	"slices"
	"strings"
)

var sdcMetaKV = KV{P: pkSM, PC: pcCyan}

type SdcMetaEntry struct {
	Name       string
	Args       []string
	CopyGlobal bool
}

func parseSdcMeta(macro TOK, args []string) SdcMetaEntry {
	unpack := slices.Contains(args, "UNPACK")
	value := "false"

	if unpack {
		value = "true"
	}

	switch macro {
	case tokSdcResourceInfo:
		if len(args) < 3 {
			throwFmt("SDC_RESOURCE_INFO expects category, name and URL")
		}

		return SdcMetaEntry{Name: args[1], CopyGlobal: true, Args: []string{"resource", "--key=" + args[0] + "/" + args[1], "--url=" + args[2], "--destination=" + args[1], "--unpack=" + value}}
	case tokSdcSbResourceInfo:
		if len(args) < 2 {
			throwFmt("SDC_SB_RESOURCE_INFO expects id and name")
		}

		return SdcMetaEntry{Name: args[1], CopyGlobal: true, Args: []string{"sb-resource", "--sb-id=" + args[0], "--name=" + args[1], "--unpack=" + value}}
	case tokSdcRosnodeInfo:
		if len(args) != 1 {
			throwFmt("SDC_ROSNODE_INFO expects one node path")
		}

		return SdcMetaEntry{Name: "rosnode", Args: []string{"rosnode", "--node-path=" + args[0]}}
	default:
		if len(args) != 1 {
			throwFmt("SDC_SOLIB_INFO expects one URL")
		}

		return SdcMetaEntry{Name: "solib", Args: []string{"dynamic-library", "--url=" + args[0]}}
	}
}

func (e *EmitContext) emitSdcMetas() []VFS {
	if len(e.d.sdcMetas) == 0 {
		return nil
	}

	toolRef, tool := e.ctx.tool(argSdgTier0ToolsBipWriter)
	na := e.ctx.na

	var globals []VFS

	for _, entry := range e.d.sdcMetas {
		out := build(e.instance.Path.relString(), "/", entry.Name, ".sdcmeta")
		global := out
		outputs := []VFS{out}
		cmds := []Cmd{{CmdArgs: na.chunkList(appendInternAnys([]ANY{tool.any()}, entry.Args)), Stdout: out, Env: envVarsVCS}}

		var scripts []VFS

		if entry.CopyGlobal {
			global = build(e.instance.Path.relString(), "/", entry.Name, ".global.sdcmeta")
			outputs = append(outputs, global)
			cmds = append(cmds, Cmd{CmdArgs: na.chunkList(na.anyList(e.d.tc.Python3.any(), copyFsToolsVFS.any(), strCopy.any(), out.any(), global.any())), Env: envVarsVCS})
			scripts = e.ctx.scripts[copyFsToolsVFS.rel()]
		}

		ref := e.ctx.emit.reserve()

		node := Node{Platform: e.instance.Platform, Cmds: cmds, Env: envVarsVCS,
			Inputs: na.inputList(na.vfsList(tool), scripts), Outputs: outputs,
			KV: &sdcMetaKV, Resources: usesPython3, DepRefs: na.refList(toolRef)}

		pending := e.pendingEmit(func() { e.emitReservedNode(node, ref) })

		for _, output := range outputs {
			e.register(GeneratedFileInfo{OutputPath: output, ProducerRef: ref, GeneratorRefs: na.refList(toolRef), OnUse: pending})
		}

		globals = append(globals, global)
	}

	return globals
}

func (e *EmitContext) appendSdcMeta(node *Node, name string) {
	if e.d.setVars[strUSE_SDC_META] != strYes {
		return
	}

	na := e.ctx.na
	script := buildInternalScriptsMergeSdcmetaPy
	out := build(e.instance.Path.relString(), "/", name, ".sdcmeta")
	args := []ANY{e.d.tc.Python3.any(), script.any(), bldRootDirVFS.any(), out.any()}

	var inputs []VFS

	for _, input := range e.globalSources {
		if strings.HasSuffix(input.relString(), ".sdcmeta") {
			args = append(args, input.any())
			inputs = append(inputs, input)
		}
	}

	node.Cmds = append(append([]Cmd(nil), node.Cmds...), Cmd{CmdArgs: na.chunkList(args), Env: envVarsVCS})
	node.Inputs = append(append(InputChunks(nil), node.Inputs...), na.vfsList(script), inputs)
	node.Outputs = append(append([]VFS(nil), node.Outputs...), out)
	e.resolveNodeCodegenDeps(node)
}

func sdcInstallLocation(packageName, kind, file string) string {
	var dir string

	switch kind {
	case "COMMON_BIN":
		dir = "common_bin"
	case "COMMON_LIB":
		dir = "common_lib"
	case "ETC_ROS":
		dir = "etc/ros"
	case "INSTALL_ROOT":
		dir = "."
	case "PROFILE_HOOKS":
		dir = "etc/catkin/profile.d"
	case "LAUNCH", "SHARE":
		return path.Join("$(B)/sdc_install/share", packageName, file)
	case "LIB":
		return path.Join("$(B)/sdc_install/lib", packageName, file)
	case "PYTHON":
		return path.Join("$(B)/sdc_install/lib/python3.6/dist-packages", packageName, file)
	case "TS":
		return path.Join("$(B)/sdc_install/share/launcher/ui/ros_type_gen/output", packageName, file)
	default:
		return ""
	}

	return path.Join("$(B)/sdc_install", dir, path.Base(file))
}

func applySdcInstall(module string, tokens []ANY, d *ModuleData, env Environment) {
	packageName := env.string(envSdcPackageName)

	if packageName == "" {
		packageName = realPrjName(module)

		if len(d.moduleStmt.Args) > 0 {
			packageName = d.moduleStmt.Args[0].string()
		}
	}

	allowed := strings.Fields(env.string(envSdcInstallKinds))
	supernode := env.string(envSdcUseSupernode) == "yes"
	args := anyStrs(tokens)
	kind := ""

	take := func() string {
		if len(args) == 0 {
			throwFmt("SDC_INSTALL: missing argument")
		}

		result := args[0]

		args = args[1:]

		return result
	}

	for len(args) > 0 {
		arg := take()

		if sdcInstallLocation(packageName, arg, "file") != "" {
			kind = arg

			continue
		}

		if kind == "" {
			throwFmt("SDC_INSTALL: expected an installation kind, got %s", arg)
		}

		enabled := len(allowed) == 0 || slices.Contains(allowed, kind)

		switch arg {
		case "TARGET", "NODE":
			target := take()
			name := path.Base(target)

			if arg == "NODE" {
				name, target = target, take()
			} else if len(args) > 0 && args[0] == "AS" {
				take()
				name = take()
			}

			out := sdcInstallLocation(packageName, kind, name)

			if !enabled {
				continue
			}

			if arg == "NODE" && supernode {
				d.runPython = append(d.runPython, &RunPythonStmt{ScriptPath: strSdgTier0ToolsGenerateSupernodeLinkPy.any(), Args: internAnys([]string{name, out}), OUTNoAutoFiles: internAnys([]string{out})})
			} else {
				d.bundles = append(d.bundles, BundleEntry{Target: target, Name: out}, BundleEntry{Target: target, Name: out + ".sdcmeta", Suffix: ".sdcmeta"})
			}
		case "PROGRAM_STARTER":
			src, name := take(), take()

			if enabled {
				out := sdcInstallLocation(packageName, kind, name)

				d.runPrograms = append(d.runPrograms, &RunProgramStmt{ToolPath: strSdgDevtoolsPyMonobinaryBuilderGenerateProgramStarter.any(), Args: internAnys([]string{src, out}), OUTNoAutoFiles: internAnys([]string{out})})
			}
		default:
			if enabled || arg == "package.xml" && slices.Contains(allowed, "SHARE") {
				d.copyFiles = append(d.copyFiles, CopyFileEntry{Src: arg, Dst: sdcInstallLocation(packageName, kind, arg)})
			}
		}
	}
}
