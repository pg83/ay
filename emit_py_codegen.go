package main

import (
	"strings"
)

func emitPySrcs(ctx *genCtx, instance ModuleInstance, d *moduleData) {
	if len(d.pySrcs) == 0 {
		return
	}

	// ENABLE(PYBUILD_NO_PYC) suppresses yapyc3 generation; modules like
	// contrib/tools/python3/lib2/py embed sources via RESOURCE/objcopy.
	if d.pyBuildNoPYC {
		return
	}

	// Walk tools/py3cc/bin and tools/py3cc/slow as HOST tools to get
	// their LD NodeRefs. Both are PROGRAM modules on x86_64.
	const (
		py3ccBinPath  = "tools/py3cc/bin"
		py3ccSlowPath = "tools/py3cc/slow"
	)

	// Walk tools/py3cc/bin (the main py3cc binary).
	// canonicalizePy3ccBinaryPath: $(B)/tools/py3cc/bin/py3cc →
	// $(B)/tools/py3cc/py3cc to match the reference yapyc3 cmd_args[0].
	// tools/py3cc/bin/ya.make declares SRCDIR(tools/py3cc) so the
	// upstream intent is a top-level binary.
	py3ccLDRef, py3ccRaw := ctx.tool(py3ccBinPath)
	py3ccBinary := canonicalizePy3ccBinary(py3ccRaw)

	py3ccSlowLDRef, py3ccSlowBin := ctx.tool(py3ccSlowPath)

	// Walk tools/rescompiler/bin, tools/rescompressor/bin, tools/archiver
	// as host tools — referenced by PY (objcopy) and AR (pyc.inc) nodes.
	// Walks are eager (memoized in ctx).
	ctx.tool("tools/rescompiler/bin")
	ctx.tool("tools/rescompressor/bin")
	ctx.tool("tools/archiver")

	// Emit one yapyc3 PY node per .py source.
	for _, srcRel := range d.pySrcs {
		if strings.HasSuffix(srcRel, ".pyi") {
			continue
		}

		generatedInputs := d.pyGeneratedSrcs[srcRel]
		srcAbs := resolveSourceVFS(ctx, instance, srcRel, d.srcDir)
		if generatedInputs != nil {
			srcAbs = Build(instance.Path + "/" + srcRel)
		}

		// The "module name" arg: <srcRel>- (trailing dash), tracking the
		// SRCDIR-resolved source path. SRCDIR(devtools/ya) + PY_SRCS(entry/
		// main.py) redirects the source to devtools/ya/entry/main.py, so
		// the module-name arg follows the resolved rel, not modulePath/srcRel.
		moduleName := srcAbs.Rel + "-"

		// Output suffix: flat → .py.yapyc3; subdir →
		// .py.<pathid($S/unit)[:4]>.yapyc3.
		var outputPath VFS
		if strings.Contains(srcRel, "/") {
			outputPath = Build(instance.Path + "/" + srcRel + "." + pySrcYapycSuffix(instance.Path) + ".yapyc3")
		} else {
			outputPath = Build(instance.Path + "/" + srcRel + ".yapyc3")
		}

		cmdArgs := []string{
			py3ccBinary.String(),
			"--slow-py3cc",
			py3ccSlowBin.String(),
			moduleName,
			srcAbs.String(),
			outputPath.String(),
		}

		env := map[string]string{
			"ARCADIA_ROOT_DISTBUILD": "$(S)",
			"PYTHONHASHSEED":         "0",
		}

		inputs := []VFS{py3ccBinary, py3ccSlowBin, srcAbs}
		if generatedInputs != nil {
			inputs = []VFS{srcAbs}
			inputs = append(inputs, generatedInputs...)
			inputs = append(inputs, py3ccBinary, py3ccSlowBin)
			if len(inputs) > 4 {
				toolA := inputs[len(inputs)-2]
				toolB := inputs[len(inputs)-1]
				copy(inputs[4:], inputs[2:len(inputs)-2])
				inputs[2] = toolA
				inputs[3] = toolB
			}
			inputs = dedupVFS(inputs)
		}

		node := &Node{
			Cmds: []Cmd{
				{
					CmdArgs: cmdArgs,
					Env:     env,
				},
			},
			Env:     env,
			Inputs:  inputs,
			Outputs: []VFS{outputPath},
			KV: map[string]string{
				"p":  "PY",
				"pc": "yellow",
			},
			Tags: instance.Platform.Tags,
			TargetProperties: func() map[string]string {
				tp := map[string]string{"module_dir": instance.Path}
				// PY23_LIBRARY's .yapyc3 nodes carry `module_tag=py3`
				// in REF (MODULE_TAG=PY3 from _ARCADIA_PYTHON3_ADDINCL
				// via the PY3 submodule). PY3_LIBRARY etc keep no tag
				// (upstream omits the redundant default).
				if d.moduleStmt.Name == "PY23_LIBRARY" {
					tp["module_tag"] = "py3"
				}
				return tp
			}(),
			Platform: string(instance.Platform.Target),
			Requirements: map[string]interface{}{
				"cpu":     float64(1),
				"network": "restricted",
				"ram":     float64(32),
			},
		}

		// Wire py3cc LD refs into both DepRefs and
		// ForeignDepRefs["tool"] to match REF. Skip zero refs
		// (host walk failed → no LD node to reference).
		var toolRefs []NodeRef

		if py3ccLDRef != (NodeRef{}) {
			node.DepRefs = append(node.DepRefs, py3ccLDRef)
			toolRefs = append(toolRefs, py3ccLDRef)
		}

		if py3ccSlowLDRef != (NodeRef{}) {
			node.DepRefs = append(node.DepRefs, py3ccSlowLDRef)
			toolRefs = append(toolRefs, py3ccSlowLDRef)
		}
		if generatedInputs != nil {
			if extras := resolveCodegenDepRefsExt(ctx, instance, nil, inputs, node.DepRefs...); len(extras) > 0 {
				node.DepRefs = append(node.DepRefs, extras...)
			}
		}

		if len(toolRefs) > 0 {
			node.ForeignDepRefs = map[string][]NodeRef{"tool": toolRefs}
		}

		pyRef := ctx.emit.Emit(node)

		// Register the .yapyc3 output in the codegen registry so the
		// downstream objcopy CC's input-driven resolveCodegenDepRefsExt
		// lookup threads the PY producer into its deps[].
		registerBoundGeneratedParsedOutput(ctx, instance, "PY", outputPath, nil, pyRef)
	}
}

// genPy3RegScriptVFS is the source-relative VFS path to the
// gen_py3_reg.py script invoked by every PY_REGISTER's PY node
// (mirror of macro _PY3_REGISTER at build/ymake.core.conf:4086-4089).
var genPy3RegScriptVFS = Source("build/scripts/gen_py3_reg.py")
var genPy3RegScriptPath = genPy3RegScriptVFS.String()

// emitPyRegister emits PY+CC pair for each PY_REGISTER(arg) in
// d.pyRegister:
//   - PY:  python3 gen_py3_reg.py <arg> $(B)/<modPath>/<arg>.reg3.cpp
//   - CC:  compiles `.reg3.cpp` → `.reg3.cpp.o` (or `.reg3.cpp.py3.o`
//     when py3Suffix is set).
//
// Both refs flow into globalRefs/globalOutputs — _PY3_REGISTER emits
// `SRCS(GLOBAL ...)`, so the CC output lands in `.global.a`.
// Mirror of _PY3_REGISTER at build/ymake.core.conf:4086-4089.
type pyRegisterResult struct {
	Refs         []NodeRef
	Outputs      []VFS
	MemberInputs []VFS
}

func emitPyRegister(ctx *genCtx, instance ModuleInstance, d *moduleData, in ModuleCCInputs, py3Suffix bool) *pyRegisterResult {
	if len(d.pyRegister) == 0 {
		return nil
	}

	res := &pyRegisterResult{}

	for i, arg := range d.pyRegister {
		// reg3[i] keeps the PyInit_/init_module_ defines contributed by the
		// explicit PY_REGISTER args declared BEFORE it: upstream invokes
		// _PY3_REGISTER once per arg, doing SRCS(GLOBAL <arg>.reg3.cpp)
		// against the current CFLAGS snapshot before onpy_register appends
		// this arg's own define, so reg3[i] sees defines[0..i-1]. Only
		// explicit PY_REGISTER args carry onpy_register defines; implicit
		// cython/swig registrations do not, so their reg3 compiles drop
		// every PyInit_/init_module_ define (shortname never in priorShort).
		priorShort := make(map[string]struct{}, i)
		for j := 0; j < i; j++ {
			if j < len(d.pyRegisterExplicit) && !d.pyRegisterExplicit[j] {
				continue
			}
			prior := d.pyRegister[j]
			priorShort[prior[strings.LastIndexByte(prior, '.')+1:]] = struct{}{}
		}

		regCpp := arg + ".reg3.cpp"
		regCppVFS := Build(instance.Path + "/" + regCpp)
		regCppAbs := regCppVFS.String()

		env := map[string]string{
			"ARCADIA_ROOT_DISTBUILD": "$(S)",
		}

		pyCmdArgs := []string{
			instance.Platform.Tools.Python3,
			genPy3RegScriptPath,
			arg,
			regCppAbs,
		}

		pyRef, ok := ctx.pyRegisterOutputs[regCppVFS]
		if !ok {
			pyInstance := instance
			pyInstance.Platform = ctx.target

			pyNode := &Node{
				Cmds: []Cmd{
					{CmdArgs: pyCmdArgs, Env: env},
				},
				Env:     env,
				Inputs:  []VFS{genPy3RegScriptVFS},
				Outputs: []VFS{regCppVFS},
				KV: map[string]string{
					"p":  "PY",
					"pc": "yellow",
				},
				Tags: []string{},
				TargetProperties: map[string]string{
					"module_dir": instance.Path,
				},
				Platform: string(pyInstance.Platform.Target),
				Requirements: map[string]interface{}{
					"cpu":     float64(1),
					"network": "restricted",
					"ram":     float64(32),
				},
				DepRefs: []NodeRef{},
			}

			if py3Suffix {
				pyNode.TargetProperties["module_tag"] = "py3"
			}

			pyRef = ctx.emit.Emit(pyNode)
			ctx.pyRegisterOutputs[regCppVFS] = pyRef
		}

		// CC node compiling `.reg3.cpp`. srcVFS = $(B)/<modPath>/<reg>
		// (regCppVFS, built above). The reference reg3 CC node lists
		// only [.reg3.cpp, gen_py3_reg.py] — no transitive header scan
		// (generated stub).
		ccIn := in
		ccIn.ExtraDepRefs = []NodeRef{pyRef}
		ccIn.Py3Suffix = py3Suffix
		if len(d.cythonCpp) > 0 {
			ccIn.AddIncl = appendCythonCCAddIncl(ccIn.AddIncl, d.cythonNumpyBeforeInclude)
		}
		ccIn.IncludeInputs = []VFS{genPy3RegScriptVFS}
		// PyInit_/init_module_ defines added by `onpy_register` AFTER
		// `_PY3_REGISTER`'s `SRCS(GLOBAL …)` attach only to user-declared
		// sources; the synthetic reg3.cpp keeps the pre-call CFLAGS
		// snapshot. Drop only the defines for THIS arg and later ones
		// (shortname not in priorShort); keep prior args' and every
		// non-PyInit flag.
		if len(in.CFlags) > 0 {
			filtered := make([]string, 0, len(in.CFlags))
			for _, f := range in.CFlags {
				if short, ok := pyInitDefineShortname(f); ok {
					if _, keep := priorShort[short]; !keep {
						continue
					}
				}
				filtered = append(filtered, f)
			}
			ccIn.CFlags = filtered
		}

		ccRef, ccOut := EmitCC(instance, regCpp, regCppVFS, ccIn, ctx.host, ctx.emit)

		res.Refs = append(res.Refs, ccRef)
		res.Outputs = append(res.Outputs, ccOut)
		// memberInputs feeds the .global.a aggregator. CC's own inputs
		// = [reg3.cpp, gen_py3_reg.py]; only gen_py3_reg.py contributes
		// (reg3.cpp is BUILD_ROOT-rooted and AR aggregator strips those).
		res.MemberInputs = append(res.MemberInputs, genPy3RegScriptVFS)
	}

	return res
}

// pyInitDefineShortname extracts the module shortname from a
// `-DPyInit_<short>=…` or `-Dinit_module_<short>=…` flag (the shortname is
// the substring between the prefix and the `=`). Returns ok=false for any
// other flag. Mirrors appendPyRegister's `-DPyInit_<shortname>` / shortname
// = segment after the last '.'.
func pyInitDefineShortname(flag string) (string, bool) {
	for _, pfx := range []string{"-DPyInit_", "-Dinit_module_"} {
		if strings.HasPrefix(flag, pfx) {
			rest := flag[len(pfx):]
			if eq := strings.IndexByte(rest, '='); eq >= 0 {
				return rest[:eq], true
			}
			return rest, true
		}
	}
	return "", false
}
