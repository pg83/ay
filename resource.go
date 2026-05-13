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
// PR-M3-resource-objcopy-A introduces the parser + emitter + hash
// scaffolding for the cluster of 127 missing PY (kv.p="PY") nodes in
// the M3 reference closure (sg2.json). PR-A's emission scope is
// narrow: simple `RESOURCE(path key)` and single-flush
// `RESOURCE_FILES(...)`; the cmd-length chunking branch
// (`MAX_CMD_LEN = 8000`) and PY_SRCS-fanout shapes land in PR-B / PR-C.

// hashLen is the prefix length applied to the MD5 hex digest when
// computing the objcopy node's output basename (matches upstream
// `packer.h:73 LEN_LIMIT = 26`).
const hashLen = 26

// rootCmdLen is the per-entry cmd-length accumulator increment used
// by upstream `TObjCopyResourcePacker::HandleResource` (`objcopy.h:22, 26`).
const rootCmdLen = 200

// maxCmdLen is the flush threshold the upstream packer applies to
// `EstimatedCmdLen_`; once exceeded the accumulator emits a node and
// resets (`objcopy.h:34, packer.h:98`). PR-A always flushes once at
// the end (single-flush only); the chunker lands in PR-C.
const maxCmdLen = 8000

// objcopyScriptPath is the SOURCE_ROOT path to the upstream packer
// script. Carried on every emitted objcopy node's `inputs` slot and
// propagated into the enclosing module's `.global.a` member inputs
// (PR-M3-globalA-narrow-closure).
const objcopyScriptPath = "$(SOURCE_ROOT)/build/scripts/objcopy.py"

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
// `build/plugins/res.py:onresource_files` expansion to the raw arg
// list of a RESOURCE_FILES(...) invocation, producing the same
// `(path, key)` packer-input pair list that a hand-written RESOURCE
// macro would. The keyword grammar is:
//
//	DONT_COMPRESS                 (flag, dropped — handled by impl.cpp)
//	PREFIX <prefix>               (per-path key prefix)
//	DEST <dest>                   (overrides prefix for the next path)
//	STRIP <prefix_to_strip>       (strips a path prefix before keying)
//	<path>                        (source path; emits one kv entry +
//	                               one path/key entry)
//
// For each plain path P:
//   - kv-only entry: Path="-",
//     Key="resfs/src/<computed-key>=${rootrel;context=TEXT;input=TEXT:\"<P>\"}".
//   - source entry: Path=<P>, Key="resfs/file/<computed-key>".
//
// computed-key = (dest) | (prefix + (strip(P) or P)). DEST takes
// precedence on a per-path basis (and resets prefix); subsequent
// paths without DEST revert to PREFIX-based keying. The
// `${rootrel;context=TEXT;input=TEXT:"<P>"}` placeholder is preserved
// verbatim because the hash formula consumes the pre-expansion form
// (verified against REF python-rapidjson objcopy hash).
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

// resourceModuleTag returns the upstream MODULE_TAG seen by the packer
// for the given module statement. Plain `LIBRARY` / `PROGRAM` and
// most module flavors set MODULE_TAG to the empty string; PY3_LIBRARY
// / PY3_PROGRAM_BIN set it to "PY3" (`build/conf/python.conf:1126
// DEFAULT(MODULE_TAG PY3)`). GEN_LIBRARY sets "RESOURCE_LIB"
// (`build/ymake.core.conf:598`); DLLs set "DLL"
// (`build/ymake.core.conf:2197, 2379`). The set here is the conservative
// PR-A subset (only `PY3` because PR-A's two scoped REF nodes belong
// to plain LIBRARY (certs) and PY3_LIBRARY (rapidjson)).
func resourceModuleTag(modName string) string {
	switch modName {
	case "PY3_LIBRARY", "PY3_PROGRAM_BIN", "PY3_PROGRAM", "PY23_LIBRARY", "PY23_NATIVE_LIBRARY":
		return "PY3"
	}

	return ""
}

// emitResourceObjcopy emits one objcopy PY node per flush of the
// upstream `TObjCopyResourcePacker`. PR-A flushes once per module
// (the chunker lands in PR-C); returned slice is the
// `$(BUILD_ROOT)/...objcopy_*.o` output paths in flush order, intended
// to be appended to the module's `.global.a` `srcs[]` by the caller.
// When the module has no parsed RESOURCE / RESOURCE_FILES entries the
// function emits nothing and returns nil.
//
// The cmd_args shape mirrors REF (verified against
// `certs/objcopy_c27c99b2d9d5eade92fd72d0aa.o` and
// `devtools/ymake/contrib/python-rapidjson/objcopy_55c44b1fdbfda511798cd895e2.o`).
// Walks `tools/rescompiler/bin` and `tools/rescompressor/bin` to
// recover their LD NodeRefs and threads them as deps; both walks are
// memoized in ctx.memo so the parallel call site in emitPySrcs does
// not double-emit.
func emitResourceObjcopy(
	ctx *genCtx,
	instance ModuleInstance,
	d *moduleData,
) ([]NodeRef, []string, []string) {
	// PR-B: emit kv_only sibling shapes (PY_MAIN, py/namespace,
	// py/no_check_imports) alongside the legacy RESOURCE/RESOURCE_FILES
	// flush. Each sibling is independent and conditional on its own
	// per-module data; a module may emit any non-empty subset.
	hasKvOnly := d.pyMain != "" || len(d.noCheckImports) > 0 || len(d.pySrcs) > 0
	if len(d.resources) == 0 && !hasKvOnly {
		return nil, nil, nil
	}

	rescompilerLDRef := walkHostToolForRef(ctx, instance, "tools/rescompiler/bin")
	rescompressorLDRef := walkHostToolForRef(ctx, instance, "tools/rescompressor/bin")

	var refs []NodeRef
	var outputs []string
	// PR-M3-globalA-narrow-closure: collect the SOURCE_ROOT-rooted
	// member inputs each emitted objcopy node would contribute to the
	// enclosing module's .global.a archive. Every emitted node carries
	// `objcopy.py` in its own `inputs[]`; path-based flushes additionally
	// carry their source paths. The kv-only sub-emitters return only
	// `objcopy.py`. Caller dedups + folds into globalMemberInputs.
	var globalMemberInputs []string
	addGlobal := func(p string) { globalMemberInputs = append(globalMemberInputs, p) }

	// kv_only siblings — each fires only when its trigger is present
	// (no-op return when not). PR-M3-py3cc-objcopy-shape: PY_MAIN fires
	// BEFORE the py/namespace resource (upstream pybuild.py:395-398
	// invokes py_main first; namespace resource is emitted later at
	// pybuild.py:587-594). Order affects the LD cmd[2] SRCS_GLOBAL slot
	// emission sequence and therefore L3 cmd-equality.
	if r, o := emitPyMainObjcopy(ctx, instance, d, rescompilerLDRef, rescompressorLDRef); o != "" {
		refs = append(refs, r)
		outputs = append(outputs, o)
		addGlobal(objcopyScriptPath)
	}

	if r, o := emitPyNamespaceObjcopy(ctx, instance, d, rescompilerLDRef, rescompressorLDRef); o != "" {
		refs = append(refs, r)
		outputs = append(outputs, o)
		addGlobal(objcopyScriptPath)
	}

	if r, o := emitNoCheckImportsObjcopy(ctx, instance, d, rescompilerLDRef, rescompressorLDRef); o != "" {
		refs = append(refs, r)
		outputs = append(outputs, o)
		addGlobal(objcopyScriptPath)
	}

	// PR-M3-resource-objcopy-C: per-PY_SRCS resfs entry objcopy nodes.
	// One node per packer-flush chunk; small modules fit in one chunk
	// (single-entry exact match); large modules (Lib, lib2/py) split
	// into multiple chunks via chunkPySrcEntries.
	srcRefs, srcOuts, srcGlobalInputs := emitPySrcObjcopy(ctx, instance, d, rescompilerLDRef, rescompressorLDRef)
	refs = append(refs, srcRefs...)
	outputs = append(outputs, srcOuts...)
	globalMemberInputs = append(globalMemberInputs, srcGlobalInputs...)

	if len(d.resources) == 0 {
		return refs, outputs, globalMemberInputs
	}

	// Filter rejected entries (mirrors objcopy.h:84-96 CanHandle):
	// drop entries whose path or name contains the BAD substrings.
	// PR-A's scoped entries never trip this; the guard mirrors upstream
	// for forward correctness.
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
		paths   []string
		keys    []string // base64-encoded (padded) keys for path entries
		kvs     []string
		cmdLen  int
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
		outputObj := "$(BUILD_ROOT)/" + instance.Path + "/objcopy_" + hash + ".o"

		cmdArgs := []string{
			"/ix/realm/pg/bin/python3",
			"$(SOURCE_ROOT)/build/scripts/objcopy.py",
			"--compiler", "/ix/realm/boot/bin/clang++",
			"--objcopy", "/ix/realm/boot/bin/llvm-objcopy",
			"--compressor", "$(BUILD_ROOT)/tools/rescompressor/rescompressor",
			"--rescompiler", "$(BUILD_ROOT)/tools/rescompiler/rescompiler",
			"--output_obj", outputObj,
			"--target", objcopyTargetTriple(instance.Platform),
		}

		// Source inputs slot: --inputs <p1> <p2> ... --keys <k1> <k2> ...
		// `paths` carry the module-relative path as declared by the macro;
		// cmd_args injects the $(SOURCE_ROOT)/<modulePath>/<path> form.
		// REF samples confirm both certs and rapidjson use this shape.
		if len(cur.paths) > 0 {
			cmdArgs = append(cmdArgs, "--inputs")
			for _, p := range cur.paths {
				cmdArgs = append(cmdArgs, "$(SOURCE_ROOT)/"+instance.Path+"/"+p)
			}
			cmdArgs = append(cmdArgs, "--keys")
			cmdArgs = append(cmdArgs, cur.keys...)
		}

		// kvs cmd_args use the POST-`${rootrel;...}`-expansion form: the
		// placeholder resolves to the module-relative path (i.e. the
		// rootrel of the source — `<unitPath>/<P>`). Hash sees the
		// pre-expansion form (cur.kvs); cmd_args sees the expanded form
		// (verified against REF rapidjson sample at
		// `cmds[0].cmd_args[25..]`).
		if len(cur.kvs) > 0 {
			cmdArgs = append(cmdArgs, "--kvs")
			for _, kv := range cur.kvs {
				cmdArgs = append(cmdArgs, expandRootrel(kv, instance.Path))
			}
		}

		env := map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)"}

		// inputs[] mirrors REF: rescompiler + rescompressor binaries,
		// then per-entry source paths in declaration order, with the
		// objcopy.py script appended last (REF certs places objcopy.py
		// before the resource paths; REF rapidjson places it after.
		// The differentiator is which slot the upstream packer adds
		// the script — `objcopy.h:50 --output_obj` references
		// `${output:__OUT}`, while the script path comes from the
		// surrounding `RUN_PYTHON3` macro template. For the single-entry
		// certs case the script lands first; for the multi-entry
		// rapidjson case the script lands after the inputs. The
		// distinguishing condition is the number of source-path entries.
		inputs := []string{
			"$(BUILD_ROOT)/tools/rescompiler/rescompiler",
			"$(BUILD_ROOT)/tools/rescompressor/rescompressor",
		}
		if len(cur.paths) <= 1 {
			inputs = append(inputs, "$(SOURCE_ROOT)/build/scripts/objcopy.py")
			for _, p := range cur.paths {
				inputs = append(inputs, "$(SOURCE_ROOT)/"+instance.Path+"/"+p)
			}
		} else {
			for _, p := range cur.paths {
				inputs = append(inputs, "$(SOURCE_ROOT)/"+instance.Path+"/"+p)
			}
			inputs = append(inputs, "$(SOURCE_ROOT)/build/scripts/objcopy.py")
		}

		// PR-M3-platform-pair-step7: tags + host_platform + platform
		// plumbed from the Platform pair on ctx; renderer does NOT branch
		// on "is host?". Empty `instance.Platform.Tags` keeps the slice non-nil so
		// JSON serialises as `[]`, not `null`.
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
			Outputs: []string{outputObj},
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

		// PR-M3-globalA-narrow-closure: this node's SOURCE_ROOT inputs
		// (per-entry source paths + objcopy.py) must propagate to the
		// enclosing module's .global.a archive's `inputs` slot.
		for _, p := range cur.paths {
			addGlobal("$(SOURCE_ROOT)/" + instance.Path + "/" + p)
		}
		addGlobal(objcopyScriptPath)

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

		// PR-A: single-flush only (no chunking). The chunker (PR-C)
		// would emit when cmdLen exceeds maxCmdLen mid-stream; PR-A
		// defers the flush to the end-of-stream `flush()` below.
		_ = maxCmdLen
	}

	flush()

	return refs, outputs, globalMemberInputs
}

// expandRootrel substitutes the upstream
// `${rootrel;context=TEXT;input=TEXT:"<P>"}` placeholder produced by
// `build/plugins/res.py:onresource_files` with its expanded form —
// the module-relative path `<unitPath>/<P>`. The placeholder is
// preserved by `expandResourceFiles` and consumed by `objcopyHash`
// pre-expansion; cmd_args consume it post-expansion (the REF
// rapidjson sample stores the expanded form in its
// `cmds[0].cmd_args` `--kvs` slot).
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

// objcopyTargetTriple maps a ModuleInstance.Target value to the
// `--target <triple>` arg understood by build/scripts/objcopy.py.
// Mirrors `objcopy.h:108-124` which parses `C_FLAGS_PLATFORM` for the
// `--target=<triple>` substring; the M2/M3 closure only carries two
// concrete triples (aarch64-linux-gnu / x86_64-linux-gnu).
//
// PR-M3-platform-pair-step12: dispatches on the Platform's Target —
// per-platform LLVM target triple, not a host/target axis decision.
func objcopyTargetTriple(p *Platform) string {
	switch p.Target {
	case PlatformDefaultLinuxX8664:
		return hostTriple
	default:
		return targetTriple
	}
}

// emitKvOnlyObjcopyNode emits a single kv_only objcopy PY node for the
// module — one whose upstream packer flush carries an empty `paths`
// slice and a non-empty `kvs` list, so the cmd_args have no `--inputs`
// / `--keys` slots and `inputs[]` is the three-element rescompiler /
// rescompressor / objcopy.py prefix (no per-resource source paths).
//
// The hash matches the upstream
// `TObjCopyResourcePacker::GetHashForOutput` derivation
// (`devtools/ymake/plugins/resource_handler/packer.h:73-85`): MD5 of
// sorted([kvsHash..., "$S/"+unitPath]) joined by "," with MODULE_TAG
// suffix; first hashLen hex chars (lower-case).
//
// Caller passes:
//   - kvsHash: the literal kv strings as the packer's hash sees them
//     after ya.make macro evaluation — i.e. with outer double quotes
//     retained for `py/namespace/...="value"` and
//     `py/no_check_imports/...="value"` (per pybuild.py:593 and
//     ytest.py:811), and unquoted for `PY_MAIN=value` (pybuild.py:759).
//   - kvsCmd: the form that lands in cmd_args after ymake's RUN_PYTHON3
//     template strips outer quotes from the kv argument tokens. Empirically
//     this is the unquoted `key=value` for all three shapes (verified
//     against REF sg2.json for the 7 PR-B nodes).
//   - moduleName: the ModuleStmt.Name (e.g. "PY3_LIBRARY"); used to
//     derive both the hash MODULE_TAG suffix (resourceModuleTag) and
//     the lower-cased target_properties.module_tag override that the
//     REF surfaces only for PY23_*-flavoured modules.
//
// Returns the `$(BUILD_ROOT)/<modulePath>/objcopy_<hash>.o` output path.
func emitKvOnlyObjcopyNode(
	ctx *genCtx,
	instance ModuleInstance,
	kvsHash []string,
	kvsCmd []string,
	moduleName string,
	rescompilerLDRef NodeRef,
	rescompressorLDRef NodeRef,
) (NodeRef, string) {
	moduleTag := resourceModuleTag(moduleName)
	hash := objcopyHash(nil, nil, kvsHash, instance.Path, moduleTag)
	outputObj := "$(BUILD_ROOT)/" + instance.Path + "/objcopy_" + hash + ".o"

	cmdArgs := []string{
		"/ix/realm/pg/bin/python3",
		"$(SOURCE_ROOT)/build/scripts/objcopy.py",
		"--compiler", "/ix/realm/boot/bin/clang++",
		"--objcopy", "/ix/realm/boot/bin/llvm-objcopy",
		"--compressor", "$(BUILD_ROOT)/tools/rescompressor/rescompressor",
		"--rescompiler", "$(BUILD_ROOT)/tools/rescompiler/rescompiler",
		"--output_obj", outputObj,
		"--target", objcopyTargetTriple(instance.Platform),
		"--kvs",
	}
	cmdArgs = append(cmdArgs, kvsCmd...)

	env := map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)"}

	// kv_only nodes always carry the three-element inputs prefix in the
	// rescompiler / rescompressor / objcopy.py order (verified against
	// all 7 REF samples in this PR's scope; no source-path entries are
	// appended because the kv-only flush has zero `paths`).
	inputs := []string{
		"$(BUILD_ROOT)/tools/rescompiler/rescompiler",
		"$(BUILD_ROOT)/tools/rescompressor/rescompressor",
		"$(SOURCE_ROOT)/build/scripts/objcopy.py",
	}

	targetProps := map[string]string{
		"module_dir": instance.Path,
	}

	// PY23_LIBRARY / PY23_NATIVE_LIBRARY have MODULE_TAG=PY3 but the
	// reference graph surfaces `target_properties.module_tag = "py3"`
	// (lower-case) only for those flavours. PY3_LIBRARY / PY3_PROGRAM_BIN
	// suppress it (the MODULE_TAG matches the default for their declared
	// type; upstream omits redundant properties). Witnessed in REF:
	// library/python/symbols/module (PY23_LIBRARY) carries module_tag=py3
	// on aarch64; library/python/runtime_py3 (PY3_LIBRARY) does not.
	switch moduleName {
	case "PY23_LIBRARY", "PY23_NATIVE_LIBRARY":
		targetProps["module_tag"] = "py3"
	}

	// PR-M3-platform-pair-step7: tags + host_platform + platform from
	// targetP. Empty `instance.Platform.Tags` keeps the slice non-nil so JSON
	// serialises as `[]`, not `null`. REF confirms: kv_only objcopy
	// nodes on x86_64 have `tags=['tool']`, aarch64 twins have `[]`.
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
		Outputs:          []string{outputObj},
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

	return ref, outputObj
}

// emitPyNamespaceObjcopy emits the `py/namespace/<mod_list_md5>/<unit>=<ns>.`
// kv objcopy node per upstream pybuild.py:587-594. The mod_list_md5 is
// a streaming md5 over each `(path, mod)` pair's `mod` UTF-8 bytes,
// iteration-ordered. The unit_path component and namespace value are
// the dotted upath. Skipped for `contrib/tools/python3` modules where
// is_extended_source_search_enabled returns False (pybuild.py:40-48),
// and for modules whose PY_SRCS is TOP_LEVEL (ns="") since ns="" emits
// no namespace kv. Returns the emitted output path (empty when nothing
// was emitted).
func emitPyNamespaceObjcopy(
	ctx *genCtx,
	instance ModuleInstance,
	d *moduleData,
	rescompilerLDRef NodeRef,
	rescompressorLDRef NodeRef,
) (NodeRef, string) {
	if len(d.pySrcs) == 0 {
		return NodeRef{}, ""
	}

	// Gate: `is_extended_source_search_enabled` returns False for
	// `$S/contrib/python*` and `$S/contrib/tools/python3*` per
	// pybuild.py:46. No namespace kv is emitted for those modules.
	if strings.HasPrefix(instance.Path, "contrib/python") ||
		strings.HasPrefix(instance.Path, "contrib/tools/python3") {
		return NodeRef{}, ""
	}

	// PR-B scope is PY3 / PY23 only; the namespace mechanism is gated
	// on py3=true in pybuild.py:559. Non-PY3 modules are skipped.
	if d.moduleStmt == nil {
		return NodeRef{}, ""
	}
	if resourceModuleTag(d.moduleStmt.Name) == "" {
		return NodeRef{}, ""
	}

	// Default namespace: `<upath-dotted>.`. The TOP_LEVEL modifier of
	// PY_SRCS empties ns; we have no per-source TOP_LEVEL tracking
	// surfaced beyond d.pySrcs, so we conservatively derive ns from
	// the upath. The four REF modules in scope do not use TOP_LEVEL
	// for the entries that drive the namespace hash (verified against
	// library/python/runtime_py3, library/python/symbols/module,
	// tools/py3cc/slow). contrib/tools/python3/lib2/py uses TOP_LEVEL
	// but is gated out by the contrib/tools/python3 check above.
	ns := strings.ReplaceAll(instance.Path, "/", ".") + "."

	// Compute mod_list_md5: streaming md5 over each `mod` in pys order.
	// mod = ns + stripext(arg).replace('/','.')   (pybuild.py:385,391).
	h := md5.New()
	for _, srcRel := range d.pySrcs {
		modName := strings.TrimSuffix(srcRel, ".py")
		modName = strings.ReplaceAll(modName, "/", ".")
		mod := ns + modName
		h.Write([]byte(mod))
	}
	modListMD5 := enchex.EncodeToString(h.Sum(nil))

	// Build the kv. mod_root_path = unit path (when SRCDIR is unset or
	// equals upath, which holds for the 3 REF modules in scope).
	// pybuild.py:591: key = '{prefix}/{md5}/{path}'; the value joins
	// sorted ns set by ':'.  Each module in scope has exactly one ns,
	// so the join collapses to that one entry verbatim.
	key := "py/namespace/" + modListMD5 + "/" + instance.Path
	// Hash form retains the outer double quotes around the value as
	// emitted by pybuild.py:593 (`'{}="{}"'.format(key, namespaces)`).
	// cmd_args form is unquoted (the RUN_PYTHON3 template strips them).
	kvHash := key + "=\"" + ns + "\""
	kvCmd := key + "=" + ns

	return emitKvOnlyObjcopyNode(ctx, instance, []string{kvHash}, []string{kvCmd}, d.moduleStmt.Name, rescompilerLDRef, rescompressorLDRef)
}

// emitPyMainObjcopy emits the `PY_MAIN=<dotted>:<func>` kv objcopy node
// per upstream pybuild.py:py_main (build/plugins/pybuild.py:759). The
// arg is captured at parse time on d.pyMain — either from the explicit
// `PY_MAIN(arg)` macro (PY_MAIN case in collectStmts) or from the
// `MAIN <src.py>` modifier of PY_SRCS (PY_SRCS case).  Returns the
// emitted output path (empty when d.pyMain is unset).
func emitPyMainObjcopy(
	ctx *genCtx,
	instance ModuleInstance,
	d *moduleData,
	rescompilerLDRef NodeRef,
	rescompressorLDRef NodeRef,
) (NodeRef, string) {
	if d.pyMain == "" {
		return NodeRef{}, ""
	}
	if d.moduleStmt == nil {
		return NodeRef{}, ""
	}

	// PY_MAIN= is unquoted in both hash and cmd_args (pybuild.py:759
	// `'PY_MAIN={}'.format(arg)` — no quotes around the value).
	kv := "PY_MAIN=" + d.pyMain
	return emitKvOnlyObjcopyNode(ctx, instance, []string{kv}, []string{kv}, d.moduleStmt.Name, rescompilerLDRef, rescompressorLDRef)
}

// emitNoCheckImportsObjcopy emits the
// `py/no_check_imports/<pathid>=<args-space-joined>` kv objcopy node
// per upstream ytest.py:on_register_no_check_imports
// (build/plugins/ytest.py:808-811). The pathid is the lower-cased
// unpadded base32 of md5(value-bytes); see build/plugins/_common.py:37
// (pathid). Returns the emitted output path (empty when d.noCheckImports
// is empty).
func emitNoCheckImportsObjcopy(
	ctx *genCtx,
	instance ModuleInstance,
	d *moduleData,
	rescompilerLDRef NodeRef,
	rescompressorLDRef NodeRef,
) (NodeRef, string) {
	if len(d.noCheckImports) == 0 {
		return NodeRef{}, ""
	}
	if d.moduleStmt == nil {
		return NodeRef{}, ""
	}

	value := strings.Join(d.noCheckImports, " ")
	sum := md5.Sum([]byte(value))
	// base32-lower-unpadded over the 16-byte digest.
	b32 := strings.ToLower(enc32.StdEncoding.EncodeToString(sum[:]))
	b32 = strings.TrimRight(b32, "=")
	key := "py/no_check_imports/" + b32
	// Hash form retains the outer double quotes (ytest.py:811
	// `'py/no_check_imports/{}="{}"'.format(...)`); cmd_args strips them.
	kvHash := key + "=\"" + value + "\""
	kvCmd := key + "=" + value

	return emitKvOnlyObjcopyNode(ctx, instance, []string{kvHash}, []string{kvCmd}, d.moduleStmt.Name, rescompilerLDRef, rescompressorLDRef)
}

// pySrcEntry is one element of the per-PY_SRCS objcopy emission. A
// single source produces between one and two entries depending on
// PYBUILD_NO_PY / PYBUILD_NO_PYC mode:
//   - default: emit both the raw .py entry AND the .yapyc3 entry
//   - PYBUILD_NO_PY (Lib): emit only the .yapyc3 entry
//   - PYBUILD_NO_PYC (lib2/py, runtime_py3, py3cc/slow): emit only the
//     raw .py entry
//
// `srcRel` is the PY_SRCS argument (relative path within the source
// unit). `actualUnit` is the SRCDIR-resolved unit (== d.srcDir when
// SRCDIR is set, otherwise == instance.Path). `topLevel` mirrors the
// PY_SRCS TOP_LEVEL prefix and strips the dotted-module-path prefix
// from the resfs key.
type pySrcEntry struct {
	pathHash  string // srcRel.yapyc3 form for flat, srcRel.3kp2.yapyc3 form for subdir; used as `paths` for the hash
	pathInput string // cmd_args --inputs slot: $(BUILD_ROOT)/<actualUnit>/<srcRel>{.suffix} (yapyc3) or $(SOURCE_ROOT)/<actualUnit>/<srcRel> (raw)
	key       string // pre-base64 key: resfs/file/py/[<ns>/]<srcRel>[.yapyc3]
	kvHash    string // pre-rootrel-expansion form (placeholder retained)
	kvCmd     string // post-rootrel-expansion form (placeholder expanded to <actualUnit>/<value>)
	inputDep  string // inputs[] graph slot: same as pathInput

	// extraSrcInput is the additional `.py` source-tree input the upstream
	// ymake scanner threads into the objcopy node's `inputs[]` whenever the
	// entry's resfs target is a `.yapyc3` bytecode (which is itself built
	// from the `.py` source by `on_py3_compile_bytecode`). For raw `.py`
	// entries this is empty (pathInput already covers the .py path). For
	// yapyc3 entries it is `$(SOURCE_ROOT)/<actualUnit>/<srcRel>`.
	// Verified against REF Lib chunks: `inputs[].py` count consistently
	// matches `inputs[].yapyc3` count one-to-one, even when the entry
	// straddles a chunk boundary (synchronize.py.3kp2.yapyc3 / synchronize.py
	// in both objcopy_0299ac47a... and objcopy_a5d68f981...).
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
		suffix := ".yapyc3"
		if strings.Contains(srcRel, "/") {
			suffix = ".3kp2.yapyc3"
		}

		// PR-M3-final-sort-inversions: when a PY_SRCS source emits both
		// the raw `.py` entry AND the `.yapyc3` entry (the
		// non-PYBUILD_NO_PY × non-PYBUILD_NO_PYC default), REF places the
		// raw `.py` entry FIRST in the packer's input list — driving
		// --inputs / --keys / --kvs ordering. Witness:
		// library/python/symbols/module/__init__.py objcopy node
		// (sg2.json) lists $(SOURCE_ROOT)/.../__init__.py before
		// $(BUILD_ROOT)/.../__init__.py.yapyc3. The chunk-hash sorts
		// internally so the objcopy_<hex>.o filename is invariant under
		// this swap; only cmd_args (and inputs[]) ordering shifts.

		// raw .py entry — emitted unless PYBUILD_NO_PY is set.
		if !d.pyBuildNoPY {
			pyKey := "resfs/file/py/" + keyPrefix + srcRel
			pyPathHash := srcRel
			pyPathInput := "$(SOURCE_ROOT)/" + actualUnit + "/" + srcRel
			pyKvHash := "resfs/src/" + pyKey + "=${rootrel;context=TEXT;input=TEXT:\"" + srcRel + "\"}"
			pyKvCmd := "resfs/src/" + pyKey + "=" + actualUnit + "/" + srcRel
			out = append(out, pySrcEntry{
				pathHash:  pyPathHash,
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
			ypPathHash := srcRel + suffix
			ypPathInput := "$(BUILD_ROOT)/" + actualUnit + "/" + srcRel + suffix
			// kv hash form retains the ${rootrel;...} placeholder with
			// the inner-srcRel-with-suffix value. cmd_args form expands
			// to <actualUnit>/<srcRel><suffix>.
			ypKvHash := "resfs/src/" + ypKey + "=${rootrel;context=TEXT;input=TEXT:\"" + srcRel + suffix + "\"}"
			ypKvCmd := "resfs/src/" + ypKey + "=" + actualUnit + "/" + srcRel + suffix
			out = append(out, pySrcEntry{
				pathHash:      ypPathHash,
				pathInput:     ypPathInput,
				key:           ypKey,
				kvHash:        ypKvHash,
				kvCmd:         ypKvCmd,
				inputDep:      ypPathInput,
				extraSrcInput: "$(SOURCE_ROOT)/" + actualUnit + "/" + srcRel,
			})
		}
	}

	return out
}

// pySrcChunk holds the accumulator state for one objcopy node emitted
// by chunkPySrcEntries — a per-HandleResource-call flush chunker
// matching upstream `TObjCopyResourcePacker::HandleResource`
// (`devtools/ymake/plugins/resource_handler/objcopy.h:17-29`).
type pySrcChunk struct {
	paths    []string
	keys     []string // base64-padded
	kvsHash  []string
	kvsCmd   []string
	pathInps []string

	// inps holds the upstream-scanner-discovered `inputs[]` fragment for
	// this chunk: every entry whose KV add OR path+key add lands in this
	// chunk contributes its `pathInput` and (when non-empty) its
	// `extraSrcInput`. The chunker emits in declaration order with
	// per-chunk deduplication; the emitter merges with the tooling prefix
	// (rescompiler/rescompressor/objcopy.py) before writing the node.
	// Captures the chunk-straddle case where an entry's KV is in chunk N
	// but its path+key is in chunk N+1 (the entry's paths land in BOTH
	// chunks' inps lists — REF byte-exact, see resource_test.go).
	inps []string
}

// chunkPySrcEntries partitions a declaration-ordered entry list into
// chunks that mirror byte-exact what the upstream packer would produce.
//
// Upstream semantics (`objcopy.h:17-29`): per RESOURCE() entry the
// `res.py:onresource_files` macro appends `[-, src_kv, path, key]` to
// the packer input. The packer's `HandleResource` is therefore called
// TWICE per entry:
//
//	(1) kv add:        EstimatedCmdLen_ += ROOT_CMD_LEN + len(src_kv_hash)
//	(2) path+key add:  EstimatedCmdLen_ += ROOT_CMD_LEN + len(path_arg) + len(b64(key))
//
// After EACH add, `Finalize(force=false)` runs; if
// `EstimatedCmdLen_ >= MAX_CMD_LEN` the chunk flushes. This is why a
// single entry can straddle a chunk boundary: the kv lands in chunk N,
// the path+key in chunk N+1.
//
// The accumulator uses the pre-expansion forms for both adds: the kv's
// `${rootrel;...}` placeholder is retained (`e.kvHash`), and the path is
// the bare source-relative string (`e.pathHash`), not the
// `$(BUILD_ROOT)/...` expansion. Confirmed byte-exact for
// `contrib/tools/python3/Lib` (40/40) and `contrib/tools/python3/lib2/py`
// (37/37 PY_SRCS chunks) — see `resource_test.go`.
func chunkPySrcEntries(entries []pySrcEntry) []pySrcChunk {
	if len(entries) == 0 {
		return nil
	}

	chunks := make([]pySrcChunk, 0)
	cur := pySrcChunk{}
	cmdLen := 0
	// inpsSeen tracks which paths have already been added to `cur.inps`
	// within the current chunk. Reset on flush. Mirrors the upstream ymake
	// scanner's natural per-node `inputs[]` dedup (a single file appears
	// once per node regardless of how many resfs entries reference it).
	// A chunk-straddling entry still contributes to both the kv-add chunk
	// and the path+key-add chunk because the map is reset between them.
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

	// addInps records the entry's pathInput + extraSrcInput on the current
	// chunk's `inps` list, deduped per-chunk by absolute path. Same-chunk
	// KV-add and path+key-add of the same entry collapse to one contribution.
	// Cross-entry duplicates (e.g. the yapyc3 entry's extraSrcInput .py and
	// the raw .py entry's pathInput naming the same file) also collapse.
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

// emitPySrcObjcopy emits one objcopy PY node per chunk of PY_SRCS-derived
// resfs entries. Returns the emitted output paths in chunk order.
// Skipped when the module has no PY_SRCS or when its PY_SRCS produces
// no resfs entries (all-suppressed by PYBUILD_NO_PY + PYBUILD_NO_PYC,
// an unobserved combination that would degenerate to a no-op).
func emitPySrcObjcopy(
	ctx *genCtx,
	instance ModuleInstance,
	d *moduleData,
	rescompilerLDRef NodeRef,
	rescompressorLDRef NodeRef,
) ([]NodeRef, []string, []string) {
	if len(d.pySrcs) == 0 {
		return nil, nil, nil
	}
	if d.moduleStmt == nil {
		return nil, nil, nil
	}

	// PY3-flavoured modules only — non-PY3 PY_SRCS does not exist in
	// the M3 closure (the gate mirrors emitPyNamespaceObjcopy:572-578).
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
	outputs := make([]string, 0, len(chunks))
	// PR-M3-globalA-narrow-closure: per-chunk SOURCE_ROOT inputs (PY_SRCS
	// raw .py paths + objcopy.py) feed the enclosing module's .global.a.
	// BUILD_ROOT-rooted .yapyc3 path entries are excluded — they are
	// non-.o codegen artefacts and the AR aggregator's filter
	// (isBuildRootCodegenProduct) would drop them anyway.
	var globalMemberInputs []string
	for _, ch := range chunks {
		hash := objcopyHash(ch.paths, ch.keys, ch.kvsHash, instance.Path, moduleTag)
		outputObj := "$(BUILD_ROOT)/" + instance.Path + "/objcopy_" + hash + ".o"

		cmdArgs := []string{
			"/ix/realm/pg/bin/python3",
			"$(SOURCE_ROOT)/build/scripts/objcopy.py",
			"--compiler", "/ix/realm/boot/bin/clang++",
			"--objcopy", "/ix/realm/boot/bin/llvm-objcopy",
			"--compressor", "$(BUILD_ROOT)/tools/rescompressor/rescompressor",
			"--rescompiler", "$(BUILD_ROOT)/tools/rescompiler/rescompiler",
			"--output_obj", outputObj,
			"--target", objcopyTargetTriple(instance.Platform),
		}

		cmdArgs = append(cmdArgs, "--inputs")
		cmdArgs = append(cmdArgs, ch.pathInps...)
		cmdArgs = append(cmdArgs, "--keys")
		cmdArgs = append(cmdArgs, ch.keys...)
		cmdArgs = append(cmdArgs, "--kvs")
		cmdArgs = append(cmdArgs, ch.kvsCmd...)

		// inputs[]: rescompiler + rescompressor + per-entry source files +
		// objcopy.py. Order from REF (multi-entry rapidjson shape, PR-A):
		// tooling first, source files, script last. PR-M3-py-objcopy-aggregation:
		// uses `ch.inps` (KV-add AND path+key-add slots, deduped per chunk)
		// instead of `ch.pathInps`; for yapyc3-only modules (Lib) this adds
		// the underlying .py source as an extra input, and on chunk-straddles
		// the spillover entry's yapyc3 + .py land in BOTH chunks' inputs[].
		inputs := []string{
			"$(BUILD_ROOT)/tools/rescompiler/rescompiler",
			"$(BUILD_ROOT)/tools/rescompressor/rescompressor",
		}
		inputs = append(inputs, ch.inps...)
		inputs = append(inputs, "$(SOURCE_ROOT)/build/scripts/objcopy.py")

		env := map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)"}

		targetProps := map[string]string{"module_dir": instance.Path}
		switch d.moduleStmt.Name {
		case "PY23_LIBRARY", "PY23_NATIVE_LIBRARY":
			targetProps["module_tag"] = "py3"
		}

		// PR-M3-platform-pair-step7: tags + host_platform + platform from
		// targetP. Empty `instance.Platform.Tags` keeps the slice non-nil so JSON
		// serialises as `[]`, not `null`.
		pyTags := []string{}
		if len(instance.Platform.Tags) > 0 {
			pyTags = append(pyTags, instance.Platform.Tags...)
		}

		node := &Node{
			Cmds:             []Cmd{{CmdArgs: cmdArgs, Env: env}},
			Env:              env,
			Inputs:           inputs,
			Outputs:          []string{outputObj},
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

		// PR-M3-L0-cascade-close-v2: thread PY (yapyc3) producer refs into
		// the objcopy CC's deps[]. The .yapyc3 paths live in `inps` as
		// BUILD_ROOT-rooted inputs; resolveCodegenDepRefsExt's input-driven
		// branch matches each against the codegen registry and returns the
		// PY producer NodeRef. Per Plan B PR-2: closes 41 PY leaves.
		exclude := []NodeRef{}
		if rescompilerLDRef != (NodeRef{}) {
			exclude = append(exclude, rescompilerLDRef)
		}
		if rescompressorLDRef != (NodeRef{}) {
			exclude = append(exclude, rescompressorLDRef)
		}
		if extras := resolveCodegenDepRefsExt(ctx, instance, nil, ch.inps, exclude...); len(extras) > 0 {
			node.DepRefs = append(node.DepRefs, extras...)
		}

		r := ctx.emit.Emit(node)
		refs = append(refs, r)
		outputs = append(outputs, outputObj)

		// SOURCE_ROOT-rooted inputs propagate into .global.a; BUILD_ROOT
		// .yapyc3 entries are filtered by the AR aggregator (PR-M3-l2).
		// PR-M3-final-surgical (fix 2): include extraSrcInput .py paths
		// (captured in ch.inps but not in ch.pathInps for yapyc3-only
		// modules like python3-Lib) so the global AR's inputs[] carries
		// every .py source the chunk's resfs entries reference.
		for _, p := range ch.inps {
			if strings.HasPrefix(p, "$(SOURCE_ROOT)/") {
				globalMemberInputs = append(globalMemberInputs, p)
			}
		}
		globalMemberInputs = append(globalMemberInputs, objcopyScriptPath)
	}

	return refs, outputs, globalMemberInputs
}

// walkHostToolForRef walks `path` as a host tool and returns the LD
// NodeRef from the memoized emit result. ParseError-class failures
// surface a zero ref (consistent with the existing host-tool walks in
// emitPySrcs); other panics propagate. Memoized in ctx.memo so back-
// to-back calls from emitPySrcs and emitResourceObjcopy do not
// duplicate node emission.
func walkHostToolForRef(ctx *genCtx, instance ModuleInstance, path string) NodeRef {
	hostInst := NewToolInstance(ctx.host, path, instance.Language)
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
