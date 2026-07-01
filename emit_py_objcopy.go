package main

import (
	"crypto/md5"
	enc32 "encoding/base32"
	encb64 "encoding/base64"
	enchex "encoding/hex"
	"sort"
	"strings"
)

var pyObjcopyKV = KV{P: pkPY, PC: pcYellow, ShowOut: true}

const (
	kvOnlyBin KvOnlyKind = iota
	kvOnlyLib
)

type ObjcopyEmitResult struct {
	Refs            []NodeRef
	Outputs         []VFS
	PySrcTrailCount int
}

func (e *EmitContext) emitResourceObjcopy() *ObjcopyEmitResult {
	ctx, instance, d := e.ctx, e.instance, e.d
	hasKvOnly := d.pyMain != nil || len(d.noCheckImports) > 0 || len(d.pySrcs) > 0 || len(d.yaConfJSON) > 0

	if len(d.resources) == 0 && len(d.pyPyiResources) == 0 && !hasKvOnly {
		return nil
	}

	oc := newObjcopyEmitCtx(ctx, d, instance.Platform)
	out := &ObjcopyEmitResult{}

	if nodeRes := e.emitPyMainObjcopy(oc); nodeRes != nil {
		out.Refs = append(out.Refs, nodeRes.Ref)
		out.Outputs = append(out.Outputs, nodeRes.Out)
	}

	if nodeRes := e.emitNoCheckImportsObjcopy(oc); nodeRes != nil {
		out.Refs = append(out.Refs, nodeRes.Ref)
		out.Outputs = append(out.Outputs, nodeRes.Out)
	}

	for _, nodeRes := range e.emitYaConfJSONObjcopy(oc) {
		out.Refs = append(out.Refs, nodeRes.Ref)
		out.Outputs = append(out.Outputs, nodeRes.Out)
	}

	if len(d.resources) == 0 && len(d.pyPyiResources) == 0 {
		trailStart := len(out.Refs)
		srcRes := e.emitPySrcObjcopy(oc)

		if srcRes != nil {
			out.Refs = append(out.Refs, srcRes.Refs...)
			out.Outputs = append(out.Outputs, srcRes.Outputs...)
		}

		out.PySrcTrailCount = len(out.Refs) - trailStart

		return out
	}

	moduleTag := resourceLibTagForData(d)

	if cfModuleTag(d, instance) == tagCppProto {
		s := strCPPProto.string()

		moduleTag = &s
	}

	py3BinProgramSide := d.moduleStmt.Name == tokPy3Program && !d.programPairedLib

	if !py3BinProgramSide {
		r, o := e.emitResourceFile(oc, d.resources, moduleTag)

		out.Refs = append(out.Refs, r...)
		out.Outputs = append(out.Outputs, o...)
	}

	trailStart := len(out.Refs)
	srcRes := e.emitPySrcObjcopy(oc)

	if srcRes != nil {
		out.Refs = append(out.Refs, srcRes.Refs...)
		out.Outputs = append(out.Outputs, srcRes.Outputs...)
	}

	if !py3BinProgramSide {
		r, o := e.emitResourceFile(oc, d.pyPyiResources, moduleTag)

		out.Refs = append(out.Refs, r...)
		out.Outputs = append(out.Outputs, o...)
	}

	out.PySrcTrailCount = len(out.Refs) - trailStart

	return out
}

func (e *EmitContext) emitResourceFile(oc *ObjcopyEmitCtx, entries []ResourceEntry, moduleTag *string) (refs []NodeRef, outputs []VFS) {
	b := newObjcopyBatcher(e, oc, ObjcopyProfile{moduleTag: moduleTag, kv: &pyObjcopyKV, layout: objcopyLayoutResource, resolveDeps: true})

	for _, entry := range entries {
		if resourceCanObjcopy(entry.Path, entry.Key) {
			if entry.Path == "-" {
				b.resourceKvEntry(entry.Key)
			} else {
				b.resourceFileEntry(entry.Path, entry.Key)
			}
		}

		b.entryDone(entry.EndsBatch)
	}

	b.flush()

	return b.results()
}

type ObjcopyEmit struct {
	Ref NodeRef
	Out VFS
}

type KvOnlyKind int

func (e *EmitContext) emitKvOnlyObjcopyNode(
	kind KvOnlyKind,
	kvsHash []string,
	kvsCmd []string,
	oc *ObjcopyEmitCtx,
) *ObjcopyEmit {
	ctx, instance, d := e.ctx, e.instance, e.d

	var moduleTag *string

	switch kind {
	case kvOnlyLib:
		moduleTag = resourceLibTagForData(d)
	default:
		moduleTag = resourceBinTagForData(d)
	}

	ref, out := buildObjcopyNode(ctx, instance, oc, ObjcopyNode{
		moduleTag: moduleTag,
		kv:        &pyObjcopyKV,
		kvsHash:   kvsHash,
		kvsCmd:    kvsCmd,
		inputs:    ctx.na.inputList(rescompilersWithScriptChunk),
		deps:      depRefs(oc.rescompilerLDRef, oc.rescompressorLDRef),
	})

	return &ObjcopyEmit{Ref: ref, Out: out}
}

func (e *EmitContext) emitYaConfJSONObjcopy(
	oc *ObjcopyEmitCtx,
) []*ObjcopyEmit {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na

	if len(d.yaConfJSON) == 0 {
		return nil
	}

	type yaConfResource struct {
		sourcePath string
		keyPath    string
		hashPath   string
	}

	var resources []yaConfResource

	for _, file := range d.yaConfJSON {
		resources = append(resources, yaConfResource{
			sourcePath: file.string(),
			keyPath:    "ya.conf.json",
			hashPath:   "ya.conf.json",
		})

		formulas := yaConfFormulaResources(ctx.fs, file.string())

		sort.Strings(formulas)

		for _, formula := range formulas {
			resources = append(resources, yaConfResource{
				sourcePath: formula,
				keyPath:    formula,
				hashPath:   formula,
			})
		}
	}

	out := make([]*ObjcopyEmit, 0, len(resources))
	moduleTag := resourceLibTagForData(d)

	for _, res := range resources {
		key := "resfs/file/" + res.keyPath
		keyB64 := encb64.StdEncoding.EncodeToString([]byte(key))
		kvHash := "resfs/src/" + key + "=${rootrel;context=TEXT;input=TEXT:\"" + res.hashPath + "\"}"
		kvCmd := "resfs/src/" + key + "=" + res.sourcePath
		input := source(res.sourcePath)

		ref, outputObj := buildObjcopyNode(ctx, instance, oc, ObjcopyNode{
			moduleTag:  moduleTag,
			kv:         &pyObjcopyKV,
			hashPaths:  []string{res.hashPath},
			keysB64:    []string{keyB64},
			kvsHash:    []string{kvHash},
			kvsCmd:     []string{kvCmd},
			pathInputs: []VFS{input},
			inputs:     na.inputList(rescompilersChunk, na.vfsList(input, objcopyScriptVFS)),
			deps:       depRefs(oc.rescompilerLDRef, oc.rescompressorLDRef),
		})

		out = append(out, &ObjcopyEmit{Ref: ref, Out: outputObj})
	}

	return out
}

func (e *EmitContext) emitPyNamespaceForGroup(
	group PySrcGroup,
	oc *ObjcopyEmitCtx,
) *ObjcopyEmit {
	ctx, instance, d := e.ctx, e.instance, e.d
	reg := e.codegen
	pySources := make([]string, 0, len(group.Srcs))
	arcSources := make([]string, 0, len(group.Srcs))

	for _, srcRel := range group.Srcs {
		if !extIsPy(srcRel.string()) {
			continue
		}

		pySources = append(pySources, srcRel.string())

		if reg.lookupSplit(dirKey(instance.Path.rel()), srcRel) == nil {
			arcSources = append(arcSources, srcRel.string())
		}
	}

	if len(pySources) == 0 || len(arcSources) == 0 {
		return nil
	}

	nsPrefix := strings.ReplaceAll(instance.Path.rel(), "/", ".") + "."

	if group.Namespace != nil {
		nsPrefix = strings.TrimSuffix(group.Namespace.string(), ".") + "."
	}

	nsValue := nsPrefix

	if group.TopLevel {
		nsPrefix = ""
		nsValue = "."
	}

	h := md5.New()

	for _, srcRel := range pySources {
		modName := strings.TrimSuffix(srcRel, ".py")

		modName = strings.ReplaceAll(modName, "/", ".")

		mod := nsPrefix + modName

		h.Write([]byte(mod))
	}

	modListMD5 := enchex.EncodeToString(h.Sum(nil))
	nsRoots := make(map[string]struct{}, len(arcSources))

	for _, srcRel := range arcSources {
		resolvedRel := resolvePySrcRel(ctx.fs, d.srcDirs, instance.Path.rel(), srcRel)
		end := len(resolvedRel) - len(srcRel) - 1

		if end < 0 {
			end = 0
		}

		nsRoots[resolvedRel[:end]] = struct{}{}
	}

	keyPaths := make([]string, 0, len(nsRoots))

	for keyPath := range nsRoots {
		keyPaths = append(keyPaths, keyPath)
	}

	sort.Strings(keyPaths)

	kvsHash := make([]string, 0, len(keyPaths))
	kvsCmd := make([]string, 0, len(keyPaths))

	for _, keyPath := range keyPaths {
		key := "py/namespace/" + modListMD5 + "/" + keyPath

		kvsHash = append(kvsHash, key+"=\""+nsValue+"\"")
		kvsCmd = append(kvsCmd, key+"="+nsValue)
	}

	return e.emitKvOnlyObjcopyNode(kvOnlyLib, kvsHash, kvsCmd, oc)
}

func (e *EmitContext) emitPyMainObjcopy(
	oc *ObjcopyEmitCtx,
) *ObjcopyEmit {
	_, _, d := e.ctx, e.instance, e.d

	if d.pyMain == nil {
		return nil
	}

	kv := "PY_MAIN=" + d.pyMain.string()

	return e.emitKvOnlyObjcopyNode(kvOnlyBin, []string{kv}, []string{kv}, oc)
}

func (e *EmitContext) emitNoCheckImportsObjcopy(
	oc *ObjcopyEmitCtx,
) *ObjcopyEmit {
	_, _, d := e.ctx, e.instance, e.d

	if len(d.noCheckImports) == 0 {
		return nil
	}

	value := strings.Join(strStrings(d.noCheckImports), " ")
	sum := md5.Sum([]byte(value))
	b32 := strings.ToLower(enc32.StdEncoding.EncodeToString(sum[:]))

	b32 = strings.TrimRight(b32, "=")

	key := "py/no_check_imports/" + b32
	kvHash := key + "=\"" + value + "\""
	kvCmd := key + "=" + value

	return e.emitKvOnlyObjcopyNode(kvOnlyBin, []string{kvHash}, []string{kvCmd}, oc)
}

func (e *EmitContext) emitPySrcObjcopy(
	oc *ObjcopyEmitCtx,
) *ObjcopyEmitResult {
	ctx, instance, d := e.ctx, e.instance, e.d

	if len(d.pySrcs) == 0 {
		return nil
	}

	if resourceLibTagForData(d) == nil {
		return nil
	}

	if d.moduleStmt.Name == tokPy3Program && !d.programPairedLib {
		return nil
	}

	groups := d.pySrcGroups

	if len(groups) == 0 {
		groups = []PySrcGroup{{Srcs: d.pySrcs, TopLevel: d.pyTopLevel, Namespace: d.pyNamespace}}
	}

	namespaceEnabled := !d.noExtendedPySearch &&
		!strings.HasPrefix(instance.Path.rel(), "contrib/python") &&
		!strings.HasPrefix(instance.Path.rel(), "contrib/tools/python3") &&
		resourceModuleTag(d.moduleStmt.Name) != nil

	moduleTag := resourceLibTagForData(d)
	res := &ObjcopyEmitResult{}

	for _, group := range groups {
		if namespaceEnabled {
			if nsRes := e.emitPyNamespaceForGroup(group, oc); nsRes != nil {
				res.Refs = append(res.Refs, nsRes.Ref)
				res.Outputs = append(res.Outputs, nsRes.Out)
			}
		}

		entries := buildPySrcEntriesFor(e.codegen, ctx.fs, d, instance.Path.rel(), strStrings(group.Srcs), group.TopLevel, group.Namespace)

		if len(entries) == 0 {
			continue
		}

		b := newObjcopyBatcher(e, oc, ObjcopyProfile{moduleTag: moduleTag, kv: &pyObjcopyKV, layout: objcopyLayoutScriptTail, resolveDeps: true})

		for _, en := range entries {
			b.kvEntry(en.kvHash, en.kvCmd, en.pathInput, en.extraInputs)
			b.fileEntry(en.pathHash, en.key, en.pathInput, en.extraInputs)
		}

		b.flush()

		groupRefs, groupOuts := b.results()

		res.Refs = append(res.Refs, groupRefs...)
		res.Outputs = append(res.Outputs, groupOuts...)
	}

	if len(res.Refs) == 0 {
		return nil
	}

	return res
}
