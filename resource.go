package main

import (
	"crypto/md5"
	enc32 "encoding/base32"
	encb64 "encoding/base64"
	enchex "encoding/hex"
	"errors"
	"sort"
	"strings"
)

// resource.go — emitter for objcopy PY nodes produced by upstream
// `devtools/ymake/plugins/resource_handler/objcopy.h:TObjCopyResourcePacker`.
// Handles `RESOURCE(path key)`, `RESOURCE_FILES(...)`, PY_SRCS chunking,
// and kv-only sibling shapes (PY_MAIN / namespace / no_check_imports).

// hashLen is the prefix length applied to the MD5 hex digest when
// computing the objcopy node's output basename (matches upstream
// `packer.h:73 LEN_LIMIT = 26`).
const hashLen = 26

// rootCmdLen is the per-entry cmd-length accumulator increment used
// by upstream `TObjCopyResourcePacker::HandleResource` (`objcopy.h:22, 26`).
const rootCmdLen = 200

// maxCmdLen is the flush threshold the upstream packer applies to
// `EstimatedCmdLen_`; once exceeded the accumulator emits a node and
// resets (`objcopy.h:34, packer.h:98`).
const maxCmdLen = 8000

// objcopyScriptPath is the SOURCE_ROOT path to the upstream packer
// script. Carried on every emitted objcopy node's `inputs` and
// propagated into the enclosing module's `.global.a` member inputs.
var (
	objcopyScriptVFS  = Source("build/scripts/objcopy.py")
	objcopyScriptPath = objcopyScriptVFS.String()

	rescompressorBinVFS  = Build("tools/rescompressor/rescompressor")
	rescompilerBinVFS    = Build("tools/rescompiler/rescompiler")
	rescompressorBinPath = rescompressorBinVFS.String()
	rescompilerBinPath   = rescompilerBinVFS.String()
)

// objcopyHash computes the upstream `GetHashForOutput` digest used
// to form the `objcopy_<hex>.o` output filename. The list is the
// concatenation of (paths, base64-padded keys, raw kvs, "$S/" + unitPath);
// it is sorted, comma-joined, suffixed with moduleTag, and MD5'd; the
// first hashLen hex chars (lower-case) are returned. Mirrors
// `devtools/ymake/plugins/resource_handler/packer.h:73-85`.
func objcopyHash(paths []string, keysB64 []string, kvs []string, unitPath, moduleTag string) string {
	list := make([]string, 0, len(paths)+len(keysB64)+len(kvs)+1)
	list = append(list, paths...)
	list = append(list, keysB64...)
	list = append(list, kvs...)
	list = append(list, "$S/"+unitPath)

	sort.Strings(list)

	stringify := strings.Join(list, ",") + moduleTag
	sum := md5.Sum([]byte(stringify))

	return strings.ToLower(enchex.EncodeToString(sum[:]))[:hashLen]
}

// expandResourceFiles applies the upstream
// `build/plugins/res.py:onresource_files` expansion. Keyword grammar:
//
//	DONT_COMPRESS (dropped), PREFIX <p>, DEST <d>, STRIP <p>, <path>.
//
// For each plain path P emits a kv-only entry (Path="-") plus a source
// entry (Path=P, Key="resfs/file/<computed-key>"). computed-key =
// (dest) | (prefix + (strip(P) or P)); DEST is per-path and resets
// prefix. The `${rootrel;...}` placeholder is preserved verbatim
// because objcopyHash consumes the pre-expansion form.
func expandResourceFiles(args []string) []resourceEntry {
	prefix := ""
	prefixToStrip := ""
	dest := ""

	out := make([]resourceEntry, 0, len(args))
	i := 0
	for i < len(args) {
		tok := args[i]
		switch tok {
		case "DONT_COMPRESS":
			i++
		case "PREFIX":
			if i+1 >= len(args) {
				ThrowFmt("RESOURCE_FILES: PREFIX is the last token; expected a prefix value")
			}
			prefix = args[i+1]
			dest = ""
			i += 2
		case "DEST":
			if i+1 >= len(args) {
				ThrowFmt("RESOURCE_FILES: DEST is the last token; expected a dest value")
			}
			dest = args[i+1]
			prefix = ""
			i += 2
		case "STRIP":
			if i+1 >= len(args) {
				ThrowFmt("RESOURCE_FILES: STRIP is the last token; expected a prefix-to-strip value")
			}
			prefixToStrip = args[i+1]
			i += 2
		default:
			path := tok
			i++

			keyTail := path
			if prefixToStrip != "" && strings.HasPrefix(keyTail, prefixToStrip) {
				keyTail = keyTail[len(prefixToStrip):]
			}

			var computedKey string
			if dest != "" {
				computedKey = dest
			} else {
				computedKey = prefix + keyTail
			}

			fileKey := "resfs/file/" + computedKey
			srcKv := "resfs/src/" + fileKey + "=${rootrel;context=TEXT;input=TEXT:\"" + path + "\"}"

			out = append(out, resourceEntry{Path: "-", Key: srcKv})
			out = append(out, resourceEntry{Path: path, Key: fileKey})
		}
	}

	return out
}

// resourceModuleTag returns the upstream MODULE_TAG seen by the packer.
// Plain LIBRARY/PROGRAM → ""; PY3_LIBRARY/PY3_PROGRAM_BIN/PY23_* → "PY3"
// (`build/conf/python.conf:1126`). GEN_LIBRARY ("RESOURCE_LIB", core
// conf:598) and DLL ("DLL", core conf:2197,2379) are not yet handled.
func resourceModuleTag(modName string) string {
	switch modName {
	case "PY3_LIBRARY", "PY3_PROGRAM_BIN", "PY3_PROGRAM", "PY23_LIBRARY", "PY23_NATIVE_LIBRARY":
		return "PY3"
	}

	return ""
}

// emitResourceObjcopy emits one objcopy PY node per flush of the
// upstream `TObjCopyResourcePacker`. Returned slice is the
// `$(B)/...objcopy_*.o` output paths in flush order, intended to be
// appended to the module's `.global.a` `srcs[]` by the caller.
//
// Walks `tools/rescompiler/bin` and `tools/rescompressor/bin` to
// recover their LD NodeRefs and threads them as deps; both walks are
// memoized in ctx.memo so the parallel call site in emitPySrcs does
// not double-emit.
func emitResourceObjcopy(
	ctx *genCtx,
	instance ModuleInstance,
	d *moduleData,
) ([]NodeRef, []VFS, []VFS) {
	// Emit kv_only sibling shapes (PY_MAIN, py/namespace,
	// py/no_check_imports) alongside RESOURCE/RESOURCE_FILES. Each
	// sibling is independent and conditional on its own per-module data.
	hasKvOnly := d.pyMain != "" || len(d.noCheckImports) > 0 || len(d.pySrcs) > 0 || len(d.yaConfJSON) > 0
	if len(d.resources) == 0 && !hasKvOnly {
		return nil, nil, nil
	}

	rescompilerLDRef := walkHostToolForRef(ctx, instance, "tools/rescompiler/bin")
	rescompressorLDRef := walkHostToolForRef(ctx, instance, "tools/rescompressor/bin")

	var refs []NodeRef
	var outputs []VFS
	// Collect the SOURCE_ROOT-rooted member inputs each emitted objcopy
	// node would contribute to the enclosing module's .global.a archive.
	// Every node carries `objcopy.py`; path-based flushes also carry
	// their source paths. Caller dedups + folds into globalMemberInputs.
	var globalMemberInputs []VFS
	addGlobal := func(p VFS) { globalMemberInputs = append(globalMemberInputs, p) }

	// kv_only siblings — each fires only when its trigger is present.
	// PY_MAIN fires BEFORE py/namespace (upstream pybuild.py:395-398
	// invokes py_main first; namespace is emitted at pybuild.py:587-594).
	// Order affects the LD cmd[2] SRCS_GLOBAL emission sequence.
	if res := emitPyMainObjcopy(ctx, instance, d, rescompilerLDRef, rescompressorLDRef); res != nil {
		refs = append(refs, res.Ref)
		outputs = append(outputs, res.Out)
		addGlobal(objcopyScriptVFS)
	}

	if res := emitPyNamespaceObjcopy(ctx, instance, d, rescompilerLDRef, rescompressorLDRef); res != nil {
		refs = append(refs, res.Ref)
		outputs = append(outputs, res.Out)
		addGlobal(objcopyScriptVFS)
	}

	if res := emitNoCheckImportsObjcopy(ctx, instance, d, rescompilerLDRef, rescompressorLDRef); res != nil {
		refs = append(refs, res.Ref)
		outputs = append(outputs, res.Out)
		addGlobal(objcopyScriptVFS)
	}

	for _, res := range emitYaConfJSONObjcopy(ctx, instance, d, rescompilerLDRef, rescompressorLDRef) {
		refs = append(refs, res.Ref)
		outputs = append(outputs, res.Out)
		addGlobal(objcopyScriptVFS)
		addGlobal(res.Input)
	}

	// Per-PY_SRCS resfs entry objcopy nodes. One node per packer-flush
	// chunk; large modules (Lib, lib2/py) split via chunkPySrcEntries.
	srcRefs, srcOuts, srcGlobalInputs := emitPySrcObjcopy(ctx, instance, d, rescompilerLDRef, rescompressorLDRef)
	refs = append(refs, srcRefs...)
	outputs = append(outputs, srcOuts...)
	globalMemberInputs = append(globalMemberInputs, srcGlobalInputs...)

	if len(d.resources) == 0 {
		return refs, outputs, globalMemberInputs
	}

	// Filter rejected entries (mirrors objcopy.h:84-96 CanHandle):
	// drop entries whose path or name contains the BAD substrings.
	bad := []string{"${ARCADIA_BUILD_ROOT}", "${ARCADIA_SOURCE_ROOT}", "conftest.py"}
	contains := func(s string) bool {
		for _, b := range bad {
			if strings.Contains(s, b) {
				return true
			}
		}

		return false
	}

	type acc struct {
		paths  []string
		keys   []string // base64-encoded (padded) keys for path entries
		kvs    []string
		cmdLen int
	}
	cur := acc{}

	moduleTag := ""
	if d.moduleStmt != nil {
		moduleTag = resourceModuleTag(d.moduleStmt.Name)
	}

	flush := func() {
		if cur.cmdLen == 0 {
			return
		}
		hash := objcopyHash(cur.paths, cur.keys, cur.kvs, instance.Path, moduleTag)
		outputObj := Build(instance.Path + "/objcopy_" + hash + ".o")

		cmdArgs := []string{
			instance.Platform.Tools.Python3,
			objcopyScriptPath,
			"--compiler", instance.Platform.Tools.CXX,
			"--objcopy", instance.Platform.Tools.Objcopy,
			"--compressor", rescompressorBinPath,
			"--rescompiler", rescompilerBinPath,
			"--output_obj", outputObj.String(),
			"--target", instance.Platform.Triple,
		}

		// Source inputs slot: --inputs <p1> ... --keys <k1> ...
		// `paths` carry the module-relative path; cmd_args injects the
		// $(S)/<modulePath>/<path> form.
		if len(cur.paths) > 0 {
			cmdArgs = append(cmdArgs, "--inputs")
			for _, p := range cur.paths {
				cmdArgs = append(cmdArgs, Source(instance.Path+"/"+p).String())
			}
			cmdArgs = append(cmdArgs, "--keys")
			cmdArgs = append(cmdArgs, cur.keys...)
		}

		// kvs cmd_args use the POST-`${rootrel;...}`-expansion form
		// (`<unitPath>/<P>`). Hash sees the pre-expansion form
		// (cur.kvs); cmd_args sees the expanded form.
		if len(cur.kvs) > 0 {
			cmdArgs = append(cmdArgs, "--kvs")
			for _, kv := range cur.kvs {
				cmdArgs = append(cmdArgs, expandRootrel(kv, instance.Path))
			}
		}

		env := map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(S)"}

		// inputs[]: rescompiler + rescompressor binaries, then per-entry
		// source paths in declaration order, with objcopy.py script
		// position toggled by source-count: ≤1 source → script before
		// the source; >1 → script after. The script path comes from the
		// surrounding `RUN_PYTHON3` macro template.
		inputs := []VFS{
			Build("tools/rescompiler/rescompiler"),
			Build("tools/rescompressor/rescompressor"),
		}
		if len(cur.paths) <= 1 {
			inputs = append(inputs, objcopyScriptVFS)
			for _, p := range cur.paths {
				inputs = append(inputs, Source(instance.Path+"/"+p))
			}
		} else {
			for _, p := range cur.paths {
				inputs = append(inputs, Source(instance.Path+"/"+p))
			}
			inputs = append(inputs, objcopyScriptVFS)
		}

		// tags + host_platform + platform plumbed from the Platform pair
		// on ctx. Empty Tags stays non-nil so JSON serialises as `[]`.
		objcopyTags := []string{}
		if len(instance.Platform.Tags) > 0 {
			objcopyTags = append(objcopyTags, instance.Platform.Tags...)
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
			Outputs: []VFS{outputObj},
			KV: map[string]string{
				"p":        "PY",
				"pc":       "yellow",
				"show_out": "yes",
			},
			Tags: objcopyTags,
			TargetProperties: map[string]string{
				"module_dir": instance.Path,
			},
			Platform:     string(instance.Platform.Target),
			HostPlatform: instance.Platform.IsHost,
			Requirements: map[string]interface{}{
				"cpu":     float64(1),
				"network": "restricted",
				"ram":     float64(32),
			},
		}

		if rescompilerLDRef != (NodeRef{}) {
			node.DepRefs = append(node.DepRefs, rescompilerLDRef)
		}

		if rescompressorLDRef != (NodeRef{}) {
			node.DepRefs = append(node.DepRefs, rescompressorLDRef)
		}

		r := ctx.emit.Emit(node)
		refs = append(refs, r)
		outputs = append(outputs, outputObj)

		// SOURCE_ROOT inputs (per-entry source paths + objcopy.py) must
		// propagate to the enclosing module's .global.a `inputs` slot.
		for _, p := range cur.paths {
			addGlobal(Source(instance.Path + "/" + p))
		}
		addGlobal(objcopyScriptVFS)

		cur = acc{}
	}

	for _, e := range d.resources {
		if !contains(e.Path) && !contains(e.Key) {
			if e.Path == "-" {
				cur.kvs = append(cur.kvs, e.Key)
				cur.cmdLen += rootCmdLen + len(e.Key)
			} else {
				cur.paths = append(cur.paths, e.Path)
				kb := encb64.StdEncoding.EncodeToString([]byte(e.Key))
				cur.keys = append(cur.keys, kb)
				cur.cmdLen += rootCmdLen + len(e.Path) + len(kb)
			}
		}

		if cur.cmdLen > maxCmdLen {
			flush()
		}
	}

	flush()

	return refs, outputs, globalMemberInputs
}

// expandRootrel substitutes the upstream
// `${rootrel;context=TEXT;input=TEXT:"<P>"}` placeholder with its
// expanded form `<unitPath>/<P>`. The placeholder is preserved by
// `expandResourceFiles` and consumed by `objcopyHash` pre-expansion;
// cmd_args consume the expanded form.
func expandRootrel(kv string, unitPath string) string {
	const marker = "${rootrel;context=TEXT;input=TEXT:\""
	idx := strings.Index(kv, marker)
	if idx < 0 {
		return kv
	}

	tail := kv[idx+len(marker):]
	end := strings.Index(tail, "\"}")
	if end < 0 {
		return kv
	}

	innerPath := tail[:end]
	expanded := unitPath + "/" + innerPath

	return kv[:idx] + expanded + tail[end+len("\"}"):]
}

// emitKvOnlyObjcopyNode emits one kv_only objcopy PY node — the packer
// flush carries empty `paths` and a non-empty `kvs` list, so cmd_args
// have no `--inputs`/`--keys` slots and `inputs[]` is the three-element
// rescompiler / rescompressor / objcopy.py prefix.
//
// Hash matches upstream `TObjCopyResourcePacker::GetHashForOutput`
// (`packer.h:73-85`): MD5 of sorted([kvsHash..., "$S/"+unitPath])
// joined by "," with MODULE_TAG suffix; first hashLen hex chars.
//
// Caller passes:
//   - kvsHash: the literal kv strings as the packer's hash sees them
//     after ya.make macro evaluation — outer double quotes retained
//     for `py/namespace/...="value"` (pybuild.py:593) and
//     `py/no_check_imports/...="value"` (ytest.py:811); unquoted for
//     `PY_MAIN=value` (pybuild.py:759).
//   - kvsCmd: the form that lands in cmd_args after RUN_PYTHON3 strips
//     outer quotes. Empirically the unquoted `key=value` for all three.
//   - moduleName: the ModuleStmt.Name; drives MODULE_TAG suffix via
//     resourceModuleTag and the lower-cased target_properties.module_tag
//     that REF surfaces only for PY23_*-flavoured modules.
func emitKvOnlyObjcopyNode(
	ctx *genCtx,
	instance ModuleInstance,
	kvsHash []string,
	kvsCmd []string,
	moduleName string,
	rescompilerLDRef NodeRef,
	rescompressorLDRef NodeRef,
) *objcopyEmit {
	moduleTag := resourceModuleTag(moduleName)
	hash := objcopyHash(nil, nil, kvsHash, instance.Path, moduleTag)
	outputObj := Build(instance.Path + "/objcopy_" + hash + ".o")

	cmdArgs := []string{
		instance.Platform.Tools.Python3,
		objcopyScriptPath,
		"--compiler", instance.Platform.Tools.CXX,
		"--objcopy", instance.Platform.Tools.Objcopy,
		"--compressor", rescompressorBinPath,
		"--rescompiler", rescompilerBinPath,
		"--output_obj", outputObj.String(),
		"--target", instance.Platform.Triple,
		"--kvs",
	}
	cmdArgs = append(cmdArgs, kvsCmd...)

	env := map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(S)"}

	// kv_only nodes always carry the three-element inputs prefix in the
	// rescompiler / rescompressor / objcopy.py order; no source-path
	// entries appended (the kv-only flush has zero `paths`).
	inputs := []VFS{
		Build("tools/rescompiler/rescompiler"),
		Build("tools/rescompressor/rescompressor"),
		objcopyScriptVFS,
	}

	targetProps := map[string]string{
		"module_dir": instance.Path,
	}

	// PY23_LIBRARY / PY23_NATIVE_LIBRARY have MODULE_TAG=PY3 but REF
	// surfaces `target_properties.module_tag="py3"` (lower-case) only
	// for those flavours; PY3_LIBRARY / PY3_PROGRAM_BIN suppress it
	// (upstream omits redundant properties matching the default).
	switch moduleName {
	case "PY23_LIBRARY", "PY23_NATIVE_LIBRARY":
		targetProps["module_tag"] = "py3"
	}

	// Empty Tags stays non-nil so JSON serialises as `[]`. REF: kv_only
	// objcopy nodes on x86_64 have `tags=['tool']`, aarch64 twins `[]`.
	kvTags := []string{}
	if len(instance.Platform.Tags) > 0 {
		kvTags = append(kvTags, instance.Platform.Tags...)
	}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           inputs,
		Outputs:          []VFS{outputObj},
		KV:               map[string]string{"p": "PY", "pc": "yellow", "show_out": "yes"},
		Tags:             kvTags,
		TargetProperties: targetProps,
		Platform:         string(instance.Platform.Target),
		HostPlatform:     instance.Platform.IsHost,
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
	}

	if rescompilerLDRef != (NodeRef{}) {
		node.DepRefs = append(node.DepRefs, rescompilerLDRef)
	}

	if rescompressorLDRef != (NodeRef{}) {
		node.DepRefs = append(node.DepRefs, rescompressorLDRef)
	}

	ref := ctx.emit.Emit(node)

	return &objcopyEmit{Ref: ref, Out: outputObj}
}

// objcopyEmit is the emit-product of a single kv-only objcopy sub-emitter
// (PY_MAIN / namespace / no_check_imports). nil = trigger absent on this
// module → nothing emitted; non-nil = (NodeRef, output path) pair.
type objcopyEmit struct {
	Ref   NodeRef
	Out   VFS
	Input VFS
}

func emitYaConfJSONObjcopy(
	ctx *genCtx,
	instance ModuleInstance,
	d *moduleData,
	rescompilerLDRef NodeRef,
	rescompressorLDRef NodeRef,
) []*objcopyEmit {
	if len(d.yaConfJSON) == 0 {
		return nil
	}

	out := make([]*objcopyEmit, 0, len(d.yaConfJSON))
	moduleTag := ""
	if d.moduleStmt != nil {
		moduleTag = resourceModuleTag(d.moduleStmt.Name)
	}

	for _, file := range d.yaConfJSON {
		key := "resfs/file/ya.conf.json"
		keyB64 := encb64.StdEncoding.EncodeToString([]byte(key))
		kvHash := "resfs/src/" + key + "=${rootrel;context=TEXT;input=TEXT:\"" + file + "\"}"
		kvCmd := "resfs/src/" + key + "=" + file
		hash := objcopyHash([]string{file}, []string{keyB64}, []string{kvHash}, instance.Path, moduleTag)
		outputObj := Build(instance.Path + "/objcopy_" + hash + ".o")
		input := Source(file)

		cmdArgs := []string{
			instance.Platform.Tools.Python3,
			objcopyScriptPath,
			"--compiler", instance.Platform.Tools.CXX,
			"--objcopy", instance.Platform.Tools.Objcopy,
			"--compressor", rescompressorBinPath,
			"--rescompiler", rescompilerBinPath,
			"--output_obj", outputObj.String(),
			"--target", instance.Platform.Triple,
			"--inputs", input.String(),
			"--keys", keyB64,
			"--kvs", kvCmd,
		}

		env := map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(S)"}
		node := &Node{
			Cmds: []Cmd{
				{
					CmdArgs: cmdArgs,
					Env:     env,
				},
			},
			Env:     env,
			Inputs:  []VFS{rescompilerBinVFS, rescompressorBinVFS, input, objcopyScriptVFS},
			Outputs: []VFS{outputObj},
			KV: map[string]string{
				"p":        "PY",
				"pc":       "yellow",
				"show_out": "yes",
			},
			Tags: []string{},
			TargetProperties: map[string]string{
				"module_dir": instance.Path,
			},
			Platform:     string(instance.Platform.Target),
			HostPlatform: instance.Platform.IsHost,
			Requirements: map[string]interface{}{
				"cpu":     float64(1),
				"network": "restricted",
				"ram":     float64(32),
			},
		}

		if len(instance.Platform.Tags) > 0 {
			node.Tags = append([]string(nil), instance.Platform.Tags...)
		}
		if rescompilerLDRef != (NodeRef{}) {
			node.DepRefs = append(node.DepRefs, rescompilerLDRef)
		}
		if rescompressorLDRef != (NodeRef{}) {
			node.DepRefs = append(node.DepRefs, rescompressorLDRef)
		}

		out = append(out, &objcopyEmit{Ref: ctx.emit.Emit(node), Out: outputObj, Input: input})
	}

	return out
}

// emitPyNamespaceObjcopy emits the `py/namespace/<mod_list_md5>/<unit>=<ns>.`
// kv objcopy node per pybuild.py:587-594. The mod_list_md5 is a
// streaming md5 over each `(path, mod)` pair's `mod` UTF-8 bytes,
// iteration-ordered. Skipped for `contrib/tools/python3*` modules
// (pybuild.py:40-48) and for modules whose PY_SRCS is TOP_LEVEL.
func emitPyNamespaceObjcopy(
	ctx *genCtx,
	instance ModuleInstance,
	d *moduleData,
	rescompilerLDRef NodeRef,
	rescompressorLDRef NodeRef,
) *objcopyEmit {
	if len(d.pySrcs) == 0 {
		return nil
	}
	if d.noExtendedPySearch {
		return nil
	}

	// Gate: `is_extended_source_search_enabled` returns False for
	// `$S/contrib/python*` and `$S/contrib/tools/python3*` per
	// pybuild.py:46. No namespace kv is emitted for those modules.
	if strings.HasPrefix(instance.Path, "contrib/python") ||
		strings.HasPrefix(instance.Path, "contrib/tools/python3") {
		return nil
	}

	// PY3 / PY23 only; gated on py3=true in pybuild.py:559.
	if d.moduleStmt == nil {
		return nil
	}
	if resourceModuleTag(d.moduleStmt.Name) == "" {
		return nil
	}

	pySources := make([]string, 0, len(d.pySrcs))
	for _, srcRel := range d.pySrcs {
		if strings.HasSuffix(srcRel, ".py") {
			pySources = append(pySources, srcRel)
		}
	}
	if len(pySources) == 0 {
		return nil
	}

	// Default namespace: `<upath-dotted>.`. TOP_LEVEL empties ns; we
	// conservatively derive ns from upath (REF modules in scope do not
	// use TOP_LEVEL for entries that drive the namespace hash).
	ns := strings.ReplaceAll(instance.Path, "/", ".") + "."

	// Compute mod_list_md5: streaming md5 over each `mod` in pys order.
	// mod = ns + stripext(arg).replace('/','.')   (pybuild.py:385,391).
	h := md5.New()
	for _, srcRel := range pySources {
		modName := strings.TrimSuffix(srcRel, ".py")
		modName = strings.ReplaceAll(modName, "/", ".")
		mod := ns + modName
		h.Write([]byte(mod))
	}
	modListMD5 := enchex.EncodeToString(h.Sum(nil))

	// Build the kv. pybuild.py:591: key = '{prefix}/{md5}/{path}';
	// value joins sorted ns set by ':' (each module in scope has one
	// ns so the join collapses to that entry).
	key := "py/namespace/" + modListMD5 + "/" + instance.Path
	// Hash form retains the outer double quotes (pybuild.py:593
	// `'{}="{}"'.format(key, namespaces)`); cmd_args strips them.
	kvHash := key + "=\"" + ns + "\""
	kvCmd := key + "=" + ns

	return emitKvOnlyObjcopyNode(ctx, instance, []string{kvHash}, []string{kvCmd}, d.moduleStmt.Name, rescompilerLDRef, rescompressorLDRef)
}

// emitPyMainObjcopy emits the `PY_MAIN=<dotted>:<func>` kv objcopy node
// per pybuild.py:759. The arg is captured at parse time on d.pyMain
// from either explicit `PY_MAIN(arg)` or the `MAIN <src.py>` modifier
// of PY_SRCS.
func emitPyMainObjcopy(
	ctx *genCtx,
	instance ModuleInstance,
	d *moduleData,
	rescompilerLDRef NodeRef,
	rescompressorLDRef NodeRef,
) *objcopyEmit {
	if d.pyMain == "" {
		return nil
	}
	if d.moduleStmt == nil {
		return nil
	}

	// PY_MAIN= is unquoted in both hash and cmd_args (pybuild.py:759
	// `'PY_MAIN={}'.format(arg)` — no quotes around the value).
	kv := "PY_MAIN=" + d.pyMain
	return emitKvOnlyObjcopyNode(ctx, instance, []string{kv}, []string{kv}, d.moduleStmt.Name, rescompilerLDRef, rescompressorLDRef)
}

// emitNoCheckImportsObjcopy emits the
// `py/no_check_imports/<pathid>=<args-space-joined>` kv objcopy node
// per ytest.py:808-811. pathid is the lower-cased unpadded base32 of
// md5(value-bytes); see build/plugins/_common.py:37.
func emitNoCheckImportsObjcopy(
	ctx *genCtx,
	instance ModuleInstance,
	d *moduleData,
	rescompilerLDRef NodeRef,
	rescompressorLDRef NodeRef,
) *objcopyEmit {
	if len(d.noCheckImports) == 0 {
		return nil
	}
	if d.moduleStmt == nil {
		return nil
	}

	value := strings.Join(d.noCheckImports, " ")
	sum := md5.Sum([]byte(value))
	// base32-lower-unpadded over the 16-byte digest.
	b32 := strings.ToLower(enc32.StdEncoding.EncodeToString(sum[:]))
	b32 = strings.TrimRight(b32, "=")
	key := "py/no_check_imports/" + b32
	// Hash form retains outer double quotes (ytest.py:811); cmd_args strips them.
	kvHash := key + "=\"" + value + "\""
	kvCmd := key + "=" + value

	return emitKvOnlyObjcopyNode(ctx, instance, []string{kvHash}, []string{kvCmd}, d.moduleStmt.Name, rescompilerLDRef, rescompressorLDRef)
}

// pySrcEntry is one element of the per-PY_SRCS objcopy emission. Per
// source: 1-2 entries by PYBUILD_NO_PY / PYBUILD_NO_PYC mode:
//   - default: raw .py entry + .yapyc3 entry
//   - PYBUILD_NO_PY: only .yapyc3
//   - PYBUILD_NO_PYC: only raw .py
type pySrcEntry struct {
	pathHash  string // srcRel.yapyc3 (flat) or srcRel.<unit-pathid>.yapyc3 (subdir); used as `paths` for the hash
	pathInput string // cmd_args --inputs slot: $(B)/<actualUnit>/<srcRel>{.suffix} (yapyc3) or $(S)/<actualUnit>/<srcRel> (raw)
	key       string // pre-base64 key: resfs/file/py/[<ns>/]<srcRel>[.yapyc3]
	kvHash    string // pre-rootrel-expansion form (placeholder retained)
	kvCmd     string // post-rootrel-expansion form
	inputDep  string // inputs[] graph slot: same as pathInput

	// extraSrcInput is the additional `.py` source-tree input the scanner
	// threads into objcopy `inputs[]` whenever the entry's resfs target
	// is a `.yapyc3` bytecode (built from the `.py` by
	// on_py3_compile_bytecode). Empty for raw .py entries; for yapyc3
	// entries `$(S)/<actualUnit>/<srcRel>`. REF Lib chunks: .py count
	// matches .yapyc3 count one-to-one even across chunk straddles.
	extraSrcInput string
}

// buildPySrcEntries derives the ordered (kv,path,key) triples that the
// upstream packer would receive for one PY_SRCS macro. The order is
// declaration order (matches REF). Each entry is independent of chunk
// boundaries — the chunker decides which chunk it lands in.
func buildPySrcEntries(d *moduleData, modulePath string) []pySrcEntry {
	if len(d.pySrcs) == 0 {
		return nil
	}

	actualUnit := modulePath
	if d.srcDir != "" {
		actualUnit = d.srcDir
	}

	// keyPrefix is the dotted-module-path prefix prepended to each
	// per-source resfs key. TOP_LEVEL strips the prefix, leaving the
	// raw source path as the resfs key root.
	keyPrefix := ""
	if !d.pyTopLevel {
		keyPrefix = modulePath + "/"
	}

	out := make([]pySrcEntry, 0, len(d.pySrcs)*2)
	for _, srcRel := range d.pySrcs {
		if strings.HasSuffix(srcRel, ".pyi") {
			continue
		}

		suffix := ".yapyc3"
		if strings.Contains(srcRel, "/") {
			suffix = "." + pySrcYapycSuffix(modulePath) + ".yapyc3"
		}

		// When both raw .py and .yapyc3 entries fire (default), REF
		// places the raw .py FIRST in the packer's input list, driving
		// --inputs / --keys / --kvs ordering. The chunk-hash sorts
		// internally so objcopy_<hex>.o is invariant under this swap;
		// only cmd_args (and inputs[]) ordering shifts.

		// raw .py entry — emitted unless PYBUILD_NO_PY is set.
		if !d.pyBuildNoPY {
			pyKey := "resfs/file/py/" + keyPrefix + srcRel
			pyPathInput := Source(actualUnit + "/" + srcRel).String()
			pyKvHash := "resfs/src/" + pyKey + "=${rootrel;context=TEXT;input=TEXT:\"" + srcRel + "\"}"
			pyKvCmd := "resfs/src/" + pyKey + "=" + actualUnit + "/" + srcRel
			out = append(out, pySrcEntry{
				pathHash:  srcRel,
				pathInput: pyPathInput,
				key:       pyKey,
				kvHash:    pyKvHash,
				kvCmd:     pyKvCmd,
				inputDep:  pyPathInput,
			})
		}

		// yapyc3 entry — always emitted unless PYBUILD_NO_PYC is set.
		if !d.pyBuildNoPYC {
			ypKey := "resfs/file/py/" + keyPrefix + srcRel + ".yapyc3"
			ypPathInput := Build(actualUnit + "/" + srcRel + suffix).String()
			// kv hash retains the ${rootrel;...} placeholder; cmd_args
			// form expands to <actualUnit>/<srcRel><suffix>.
			ypKvHash := "resfs/src/" + ypKey + "=${rootrel;context=TEXT;input=TEXT:\"" + srcRel + suffix + "\"}"
			ypKvCmd := "resfs/src/" + ypKey + "=" + actualUnit + "/" + srcRel + suffix
			out = append(out, pySrcEntry{
				pathHash:      srcRel + suffix,
				pathInput:     ypPathInput,
				key:           ypKey,
				kvHash:        ypKvHash,
				kvCmd:         ypKvCmd,
				inputDep:      ypPathInput,
				extraSrcInput: Source(actualUnit + "/" + srcRel).String(),
			})
		}
	}

	return out
}

// pySrcChunk holds the accumulator state for one objcopy node emitted
// by chunkPySrcEntries — per-HandleResource-call flush chunker matching
// upstream `TObjCopyResourcePacker::HandleResource` (`objcopy.h:17-29`).
type pySrcChunk struct {
	paths    []string
	keys     []string // base64-padded
	kvsHash  []string
	kvsCmd   []string
	pathInps []string

	// inps holds the scanner-discovered `inputs[]` fragment for this
	// chunk: every entry whose KV add OR path+key add lands here
	// contributes its `pathInput` and (when non-empty) `extraSrcInput`,
	// in declaration order with per-chunk dedup. Captures chunk-straddle
	// where an entry's KV is in chunk N but its path+key is in N+1
	// (REF byte-exact, see resource_test.go).
	inps []string
}

// chunkPySrcEntries partitions a declaration-ordered entry list into
// chunks byte-exact with the upstream packer.
//
// Upstream semantics (`objcopy.h:17-29`): per RESOURCE() entry the
// `res.py:onresource_files` macro appends `[-, src_kv, path, key]` so
// `HandleResource` is called TWICE per entry:
//
//	(1) kv add:       EstimatedCmdLen_ += ROOT_CMD_LEN + len(src_kv_hash)
//	(2) path+key add: EstimatedCmdLen_ += ROOT_CMD_LEN + len(path_arg) + len(b64(key))
//
// After each add, `Finalize(force=false)` flushes when
// EstimatedCmdLen_ >= MAX_CMD_LEN — a single entry can straddle a
// chunk boundary (kv in N, path+key in N+1).
//
// Accumulator uses pre-expansion forms: kv keeps `${rootrel;...}`
// (e.kvHash); path is the bare source-relative string (e.pathHash).
func chunkPySrcEntries(entries []pySrcEntry) []pySrcChunk {
	if len(entries) == 0 {
		return nil
	}

	chunks := make([]pySrcChunk, 0)
	cur := pySrcChunk{}
	cmdLen := 0
	// inpsSeen tracks paths already in `cur.inps` for the current chunk
	// (reset on flush). A chunk-straddling entry still contributes to
	// both kv-add chunk and path+key-add chunk because the map resets
	// between them.
	inpsSeen := make(map[string]struct{})

	flush := func() {
		if cmdLen == 0 {
			return
		}
		chunks = append(chunks, cur)
		cur = pySrcChunk{}
		cmdLen = 0
		inpsSeen = make(map[string]struct{})
	}

	// addInps records pathInput + extraSrcInput on `cur.inps`, deduped
	// per-chunk by absolute path. Collapses same-chunk KV/path+key adds
	// and cross-entry duplicates (yapyc3.extraSrcInput vs raw .py).
	addInps := func(e pySrcEntry) {
		if _, ok := inpsSeen[e.pathInput]; !ok {
			cur.inps = append(cur.inps, e.pathInput)
			inpsSeen[e.pathInput] = struct{}{}
		}
		if e.extraSrcInput == "" {
			return
		}
		if _, ok := inpsSeen[e.extraSrcInput]; !ok {
			cur.inps = append(cur.inps, e.extraSrcInput)
			inpsSeen[e.extraSrcInput] = struct{}{}
		}
	}

	for _, e := range entries {
		// HandleResource("-", e.kvHash): kv add.
		cur.kvsHash = append(cur.kvsHash, e.kvHash)
		cur.kvsCmd = append(cur.kvsCmd, e.kvCmd)
		addInps(e)
		cmdLen += rootCmdLen + len(e.kvHash)
		if cmdLen >= maxCmdLen {
			flush()
		}

		// HandleResource(e.pathHash, e.key): path+key add.
		kb64 := encb64.StdEncoding.EncodeToString([]byte(e.key))
		cur.paths = append(cur.paths, e.pathHash)
		cur.keys = append(cur.keys, kb64)
		cur.pathInps = append(cur.pathInps, e.pathInput)
		addInps(e)
		cmdLen += rootCmdLen + len(e.pathHash) + len(kb64)
		if cmdLen >= maxCmdLen {
			flush()
		}
	}
	flush()

	return chunks
}

func pySrcYapycSuffix(modulePath string) string {
	return protoPathID("$S/" + modulePath)[:4]
}

// emitPySrcObjcopy emits one objcopy PY node per chunk of PY_SRCS-derived
// resfs entries. Skipped when no PY_SRCS or when all entries are
// suppressed by PYBUILD_NO_PY + PYBUILD_NO_PYC.
func emitPySrcObjcopy(
	ctx *genCtx,
	instance ModuleInstance,
	d *moduleData,
	rescompilerLDRef NodeRef,
	rescompressorLDRef NodeRef,
) ([]NodeRef, []VFS, []VFS) {
	if len(d.pySrcs) == 0 {
		return nil, nil, nil
	}
	if d.moduleStmt == nil {
		return nil, nil, nil
	}

	// PY3-flavoured modules only — gate mirrors emitPyNamespaceObjcopy.
	if resourceModuleTag(d.moduleStmt.Name) == "" {
		return nil, nil, nil
	}

	entries := buildPySrcEntries(d, instance.Path)
	if len(entries) == 0 {
		return nil, nil, nil
	}

	chunks := chunkPySrcEntries(entries)
	moduleTag := resourceModuleTag(d.moduleStmt.Name)

	refs := make([]NodeRef, 0, len(chunks))
	outputs := make([]VFS, 0, len(chunks))
	// Per-chunk SOURCE_ROOT inputs (PY_SRCS raw .py paths + objcopy.py)
	// feed the enclosing module's .global.a. BUILD_ROOT-rooted .yapyc3
	// entries are excluded — codegen artefacts that the AR aggregator's
	// isBuildRootCodegenProduct filter would drop anyway.
	var globalMemberInputs []VFS
	for _, ch := range chunks {
		hash := objcopyHash(ch.paths, ch.keys, ch.kvsHash, instance.Path, moduleTag)
		outputObj := Build(instance.Path + "/objcopy_" + hash + ".o")

		cmdArgs := []string{
			instance.Platform.Tools.Python3,
			objcopyScriptPath,
			"--compiler", instance.Platform.Tools.CXX,
			"--objcopy", instance.Platform.Tools.Objcopy,
			"--compressor", rescompressorBinPath,
			"--rescompiler", rescompilerBinPath,
			"--output_obj", outputObj.String(),
			"--target", instance.Platform.Triple,
		}

		cmdArgs = append(cmdArgs, "--inputs")
		cmdArgs = append(cmdArgs, ch.pathInps...)
		cmdArgs = append(cmdArgs, "--keys")
		cmdArgs = append(cmdArgs, ch.keys...)
		cmdArgs = append(cmdArgs, "--kvs")
		cmdArgs = append(cmdArgs, ch.kvsCmd...)

		// inputs[]: rescompiler + rescompressor + per-entry source files
		// + objcopy.py (script last). Uses `ch.inps` (KV and path+key
		// slots, deduped per chunk); for yapyc3-only modules this adds
		// the underlying .py; on chunk straddles the spillover entry's
		// yapyc3 + .py land in BOTH chunks' inputs[].
		inputs := []VFS{
			Build("tools/rescompiler/rescompiler"),
			Build("tools/rescompressor/rescompressor"),
		}
		for _, p := range ch.inps {
			inputs = append(inputs, ParseVFSOrSource(p))
		}
		inputs = append(inputs, objcopyScriptVFS)

		env := map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(S)"}

		targetProps := map[string]string{"module_dir": instance.Path}
		switch d.moduleStmt.Name {
		case "PY23_LIBRARY", "PY23_NATIVE_LIBRARY":
			targetProps["module_tag"] = "py3"
		}

		// Empty Tags stays non-nil so JSON serialises as `[]`.
		pyTags := []string{}
		if len(instance.Platform.Tags) > 0 {
			pyTags = append(pyTags, instance.Platform.Tags...)
		}

		node := &Node{
			Cmds:             []Cmd{{CmdArgs: cmdArgs, Env: env}},
			Env:              env,
			Inputs:           inputs,
			Outputs:          []VFS{outputObj},
			KV:               map[string]string{"p": "PY", "pc": "yellow", "show_out": "yes"},
			Tags:             pyTags,
			TargetProperties: targetProps,
			Platform:         string(instance.Platform.Target),
			HostPlatform:     instance.Platform.IsHost,
			Requirements: map[string]interface{}{
				"cpu":     float64(1),
				"network": "restricted",
				"ram":     float64(32),
			},
		}

		if rescompilerLDRef != (NodeRef{}) {
			node.DepRefs = append(node.DepRefs, rescompilerLDRef)
		}
		if rescompressorLDRef != (NodeRef{}) {
			node.DepRefs = append(node.DepRefs, rescompressorLDRef)
		}

		// Thread PY (yapyc3) producer refs into the objcopy node's deps[].
		// The .yapyc3 paths live in `inps` as BUILD_ROOT-rooted inputs;
		// resolveCodegenDepRefsExt's input-driven branch matches each
		// against the codegen registry and returns the PY producer ref.
		exclude := []NodeRef{}
		if rescompilerLDRef != (NodeRef{}) {
			exclude = append(exclude, rescompilerLDRef)
		}
		if rescompressorLDRef != (NodeRef{}) {
			exclude = append(exclude, rescompressorLDRef)
		}
		chInpsVFS := make([]VFS, 0, len(ch.inps))
		for _, p := range ch.inps {
			chInpsVFS = append(chInpsVFS, ParseVFSOrSource(p))
		}
		if extras := resolveCodegenDepRefsExt(ctx, instance, nil, chInpsVFS, exclude...); len(extras) > 0 {
			node.DepRefs = append(node.DepRefs, extras...)
		}

		r := ctx.emit.Emit(node)
		refs = append(refs, r)
		outputs = append(outputs, outputObj)

		// SOURCE_ROOT-rooted inputs propagate into .global.a; BUILD_ROOT
		// .yapyc3 entries are filtered by the AR aggregator. Include
		// extraSrcInput .py paths (in ch.inps but not ch.pathInps for
		// yapyc3-only modules like python3-Lib) so the global AR's
		// inputs[] carries every .py source the chunk references.
		for _, p := range ch.inps {
			if strings.HasPrefix(p, "$(S)/") {
				globalMemberInputs = append(globalMemberInputs, ParseVFSOrSource(p))
			}
		}
		globalMemberInputs = append(globalMemberInputs, objcopyScriptVFS)
	}

	return refs, outputs, globalMemberInputs
}

// walkHostToolForRef walks `path` as a host tool and returns the LD
// NodeRef. ParseError-class failures surface a zero ref; other panics
// propagate. Memoized in ctx.memo so back-to-back calls from emitPySrcs
// and emitResourceObjcopy do not duplicate node emission.
func walkHostToolForRef(ctx *genCtx, instance ModuleInstance, path string) NodeRef {
	hostInst := NewToolInstance(ctx.host, path)
	hostInst.Flags = inferFlagsFromPath(path, true)

	var ref NodeRef
	if exc := Try(func() {
		result := genModule(ctx, hostInst)
		ref = result.LDRef
	}); exc != nil {
		var pe *ParseError
		if !errors.As(exc.AsError(), &pe) {
			panic(exc)
		}
	}

	return ref
}
