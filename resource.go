package main

import (
	"crypto/md5"
	encb64 "encoding/base64"
	enchex "encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
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
func objcopyHash(paths []string, keysB64 []string, kvs []string, unitPath string, moduleTag *string) string {
	list := make([]string, 0, len(paths)+len(keysB64)+len(kvs)+1)
	list = append(list, paths...)
	list = append(list, keysB64...)
	list = append(list, kvs...)
	list = append(list, "$S/"+unitPath)

	sort.Strings(list)

	stringify := strings.Join(list, ",")
	if moduleTag != nil {
		stringify += *moduleTag
	}
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
// Plain LIBRARY/PROGRAM → nil. PY3_LIBRARY/PY23_* → "PY3".
// PY3_PROGRAM_BIN is the executable half of PY3_PROGRAM; upstream surfaces
// `module_tag=py3_bin` there.
// (`build/conf/python.conf:1126`). GEN_LIBRARY ("RESOURCE_LIB", core
// conf:598) and DLL ("DLL", core conf:2197,2379) are not yet handled.
func resourceModuleTag(modName string) *string {
	switch modName {
	case "PY3_LIBRARY", "PY3_PROGRAM_BIN", "PY23_LIBRARY", "PY23_NATIVE_LIBRARY":
		return stringPtr("PY3")
	}

	return nil
}

func resourceModuleTagForData(d *moduleData) *string {
	if d == nil || d.moduleStmt == nil {
		return nil
	}
	if d.py3ProgramMultimodule && d.moduleStmt.Name == "PY3_PROGRAM_BIN" {
		return stringPtr("PY3_BIN")
	}
	return resourceModuleTag(d.moduleStmt.Name)
}

func prResourceExtraInputs(d *moduleData, output string) []VFS {
	if d == nil || d.prOutputInputs == nil {
		return nil
	}

	inputs := d.prOutputInputs[output]
	out := make([]VFS, 0, len(inputs))
	for _, p := range inputs {
		if p.IsSource() {
			out = append(out, p)
		}
	}

	return out
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

func yaConfFormulaResources(sourceRoot string, confPath string) []string {
	if sourceRoot == "" {
		return nil
	}

	raw, err := os.ReadFile(filepath.Join(sourceRoot, filepath.FromSlash(confPath)))
	if err != nil {
		return nil
	}

	var out []string
	seen := map[string]struct{}{}
	for _, m := range yaConfFormulaRE.FindAllSubmatch(raw, -1) {
		formula := string(m[1])
		if _, dup := seen[formula]; dup {
			continue
		}
		seen[formula] = struct{}{}
		out = append(out, formula)
	}

	return out
}

var yaConfFormulaRE = regexp.MustCompile(`"formula"\s*:\s*"([^"]+\.json)"`)

// pySrcEntry is one element of the per-PY_SRCS objcopy emission. Per
// source: 1-2 entries by PYBUILD_NO_PY / PYBUILD_NO_PYC mode:
//   - default: raw .py entry + .yapyc3 entry
//   - PYBUILD_NO_PY: only .yapyc3
//   - PYBUILD_NO_PYC: only raw .py
type pySrcEntry struct {
	pathHash  string // srcRel.yapyc3 (flat) or srcRel.<unit-pathid>.yapyc3 (subdir); used as `paths` for the hash
	pathInput VFS    // cmd_args --inputs slot: $(B)/<actualUnit>/<srcRel>{.suffix} (yapyc3) or $(S)/<actualUnit>/<srcRel> (raw)
	key       string // pre-base64 key: resfs/file/py/[<ns>/]<srcRel>[.yapyc3]
	kvHash    string // pre-rootrel-expansion form (placeholder retained)
	kvCmd     string // post-rootrel-expansion form
	inputDep  VFS    // inputs[] graph slot: same as pathInput

	// extraSrcInput is the additional `.py` source-tree input the scanner
	// threads into objcopy `inputs[]` whenever the entry's resfs target
	// is a `.yapyc3` bytecode (built from the `.py` by
	// on_py3_compile_bytecode). Nil for raw .py entries; for yapyc3
	// entries non-nil `$(S)/<actualUnit>/<srcRel>`. REF Lib chunks: .py count
	// matches .yapyc3 count one-to-one even across chunk straddles.
	extraSrcInput *VFS
}

// buildPySrcEntries derives the ordered (kv,path,key) triples that the
// upstream packer would receive for one PY_SRCS macro. The order is
// declaration order (matches REF). Each entry is independent of chunk
// boundaries — the chunker decides which chunk it lands in.
func buildPySrcEntries(d *moduleData, modulePath string) []pySrcEntry {
	return buildPySrcEntriesFor(d, modulePath, d.pySrcs, d.pyTopLevel, d.pyNamespace)
}

func buildPySrcEntriesFor(d *moduleData, modulePath string, srcs []string, topLevel bool, namespace *string) []pySrcEntry {
	if len(srcs) == 0 {
		return nil
	}

	actualUnit := modulePath
	if d.srcDir != nil {
		actualUnit = *d.srcDir
	}

	// keyPrefix is the dotted-module-path prefix prepended to each
	// per-source resfs key. TOP_LEVEL strips the prefix, leaving the
	// raw source path as the resfs key root.
	keyPrefix := ""
	if !topLevel {
		if namespace != nil {
			keyPrefix = strings.ReplaceAll(strings.TrimSuffix(*namespace, "."), ".", "/") + "/"
		} else {
			keyPrefix = modulePath + "/"
		}
	}

	out := make([]pySrcEntry, 0, len(srcs)*2)
	for _, srcRel := range srcs {
		if strings.HasSuffix(srcRel, ".pyi") {
			continue
		}
		if d.pyGeneratedSrcs[srcRel] != nil {
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
			pyPathInput := Source(actualUnit + "/" + srcRel)
			if d.pyGeneratedSrcs[srcRel] != nil {
				pyPathInput = Build(modulePath + "/" + srcRel)
			}
			pyKvHash := "resfs/src/" + pyKey + "=${rootrel;context=TEXT;input=TEXT:\"" + srcRel + "\"}"
			pyKvCmd := "resfs/src/" + pyKey + "=" + actualUnit + "/" + srcRel
			if d.pyGeneratedSrcs[srcRel] != nil {
				pyKvCmd = "resfs/src/" + pyKey + "=" + modulePath + "/" + srcRel
			}
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
			ypPathInput := Build(modulePath + "/" + srcRel + suffix)
			// kv hash retains the ${rootrel;...} placeholder; cmd_args
			// form expands to <actualUnit>/<srcRel><suffix>.
			ypKvHash := "resfs/src/" + ypKey + "=${rootrel;context=TEXT;input=TEXT:\"" + srcRel + suffix + "\"}"
			ypKvCmd := "resfs/src/" + ypKey + "=" + modulePath + "/" + srcRel + suffix
			extraSrcInput := vfsPtr(Source(actualUnit + "/" + srcRel))
			if d.pyGeneratedSrcs[srcRel] != nil {
				ypKvCmd = "resfs/src/" + ypKey + "=" + modulePath + "/" + srcRel + suffix
				extraSrcInput = vfsPtr(Build(modulePath + "/" + srcRel))
			}
			out = append(out, pySrcEntry{
				pathHash:      srcRel + suffix,
				pathInput:     ypPathInput,
				key:           ypKey,
				kvHash:        ypKvHash,
				kvCmd:         ypKvCmd,
				inputDep:      ypPathInput,
				extraSrcInput: extraSrcInput,
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
	// contributes its `pathInput` and (when present) `extraSrcInput`,
	// in declaration order with per-chunk dedup. Captures chunk-straddle
	// where an entry's KV is in chunk N but its path+key is in N+1
	// (REF byte-exact, see resource_test.go).
	inps []VFS
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
	inpsSeen := make(map[VFS]struct{})

	flush := func() {
		if cmdLen == 0 {
			return
		}
		chunks = append(chunks, cur)
		cur = pySrcChunk{}
		cmdLen = 0
		inpsSeen = make(map[VFS]struct{})
	}

	// addInps records pathInput + extraSrcInput on `cur.inps`, deduped
	// per-chunk by absolute path. Collapses same-chunk KV/path+key adds
	// and cross-entry duplicates (yapyc3.extraSrcInput vs raw .py).
	addInps := func(e pySrcEntry) {
		if _, ok := inpsSeen[e.pathInput]; !ok {
			cur.inps = append(cur.inps, e.pathInput)
			inpsSeen[e.pathInput] = struct{}{}
		}
		if e.extraSrcInput == nil {
			return
		}
		if _, ok := inpsSeen[*e.extraSrcInput]; !ok {
			cur.inps = append(cur.inps, *e.extraSrcInput)
			inpsSeen[*e.extraSrcInput] = struct{}{}
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
		cur.pathInps = append(cur.pathInps, e.pathInput.String())
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
