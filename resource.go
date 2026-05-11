package main

import (
	"crypto/md5"
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
) []string {
	if len(d.resources) == 0 {
		return nil
	}

	rescompilerLDRef := walkHostToolForRef(ctx, instance, "tools/rescompiler/bin")
	rescompressorLDRef := walkHostToolForRef(ctx, instance, "tools/rescompressor/bin")

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

	var outputs []string

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
			"--target", objcopyTargetTriple(instance),
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
			Tags: []string{},
			TargetProperties: map[string]string{
				"module_dir": instance.Path,
			},
			Platform: string(instance.Target),
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

		ctx.emit.Emit(node)
		outputs = append(outputs, outputObj)

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

	return outputs
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
func objcopyTargetTriple(instance ModuleInstance) string {
	if targetIsX8664(instance) {
		return hostTriple
	}

	return targetTriple
}

// walkHostToolForRef walks `path` as a host tool and returns the LD
// NodeRef from the memoized emit result. ParseError-class failures
// surface a zero ref (consistent with the existing host-tool walks in
// emitPySrcs); other panics propagate. Memoized in ctx.memo so back-
// to-back calls from emitPySrcs and emitResourceObjcopy do not
// duplicate node emission.
func walkHostToolForRef(ctx *genCtx, instance ModuleInstance, path string) NodeRef {
	hostInst := instance.WithHost(ctx.cfg)
	hostInst.Path = path
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
