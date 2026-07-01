package main

import (
	"crypto/md5"
	encb64 "encoding/base64"
	enchex "encoding/hex"
	"sort"
	"strings"
)

var (
	objcopyScriptPath           = objcopyScriptVFS.string()
	rescompressorBinPath        = rescompressorBinVFS.string()
	rescompilerBinPath          = rescompilerBinVFS.string()
	rescompilersChunk           = []VFS{rescompilerBinVFS, rescompressorBinVFS}
	rescompilersWithScriptChunk = []VFS{rescompilerBinVFS, rescompressorBinVFS, objcopyScriptVFS}
	objcopyScriptChunk          = []VFS{objcopyScriptVFS}
	rawAuxKV                    = KV{P: pkPR, PC: pcYellow, ShowOut: true}
	batchDeduper                DeDuper
	pyObjcopyKV                 = KV{P: pkPY, PC: pcYellow, ShowOut: true}
)

const (
	hashLen    = 26
	rootCmdLen = 200
	maxCmdLen  = 8000
)

const (
	objcopyLayoutResource ObjcopyLayout = iota
	objcopyLayoutScriptTail
)

func resourceCanObjcopy(path, key string) bool {
	for _, bad := range []string{"${ARCADIA_BUILD_ROOT}", "${ARCADIA_SOURCE_ROOT}", "conftest.py"} {
		if strings.Contains(path, bad) || strings.Contains(key, bad) {
			return false
		}
	}

	return true
}

func resourcePackHash(items []string, unitPath, moduleTag string) string {
	list := append(make([]string, 0, len(items)+1), items...)

	list = append(list, unitPath)

	sort.Strings(list)

	sum := md5.Sum([]byte(strings.Join(list, ",") + moduleTag))

	return strings.ToLower(enchex.EncodeToString(sum[:]))[:hashLen]
}

func objcopyHash(paths []string, keysB64 []string, kvs []string, unitPath string, moduleTag *string) string {
	list := make([]string, 0, len(paths)+len(keysB64)+len(kvs))

	list = append(list, paths...)
	list = append(list, keysB64...)
	list = append(list, kvs...)

	tag := ""

	if moduleTag != nil {
		tag = *moduleTag
	}

	return resourcePackHash(list, "$S/"+unitPath, tag)
}

func renderResourceKvCmd(kv string) string {
	kv = strings.ReplaceAll(kv, "${ARCADIA_ROOT}/", "$(S)/")
	kv = strings.ReplaceAll(kv, "${ARCADIA_BUILD_ROOT}/", "$(B)/")

	return kv
}

func rootrelExpand(kv string, resolved string) string {
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

	return kv[:idx] + resolved + tail[end+len("\"}"):]
}

func rootrelInputPath(kv string) (string, bool) {
	const marker = "${rootrel;context=TEXT;input=TEXT:\""

	idx := strings.Index(kv, marker)

	if idx < 0 {
		return "", false
	}

	tail := kv[idx+len(marker):]
	end := strings.Index(tail, "\"}")

	if end < 0 {
		return "", false
	}

	return tail[:end], true
}

type ObjcopyArgBlocks struct {
	pre  []STR
	post []STR
}

type ObjcopyEmitCtx struct {
	rescompilerLDRef   NodeRef
	rescompressorLDRef NodeRef
	blocks             ObjcopyArgBlocks
	na                 *NodeArenas
}

func newObjcopyEmitCtx(ctx *GenCtx, d *ModuleData, p *Platform) *ObjcopyEmitCtx {
	oc := &ObjcopyEmitCtx{na: ctx.na}

	oc.rescompilerLDRef, _ = ctx.tool(argToolsRescompiler)
	oc.rescompressorLDRef, _ = ctx.tool(argToolsRescompressor)
	oc.blocks = composeObjcopyArgBlocks(d.tc, p)

	return oc
}

func composeObjcopyArgBlocks(tc ModuleToolchain, p *Platform) ObjcopyArgBlocks {
	return ObjcopyArgBlocks{
		pre: []STR{
			tc.Python3,
			internStr(objcopyScriptPath),
			argCompiler.str(), tc.CXX,
			argObjcopy.str(), tc.Objcopy,
			argCompressor.str(), internStr(rescompressorBinPath),
			argRescompiler.str(), internStr(rescompilerBinPath),
			argOutputObj.str(),
		},
		post: []STR{argTarget.str(), internStr(p.Triple)},
	}
}

func objcopyCmdArgs(oc *ObjcopyEmitCtx, outputObj VFS, payload []STR) ArgChunks {
	return oc.na.chunkList(oc.blocks.pre, oc.na.strList((outputObj).str()), oc.blocks.post, payload)
}

type ResolvedResource struct {
	Input           VFS
	ProducerRef     NodeRef
	ProducerMainOut VFS
	SourceInputs    []VFS
}

func (e *EmitContext) resolveResourceInput(rawPath string, fallback VFS) ResolvedResource {
	_, instance := e.ctx, e.instance
	output := resourceOutputVFS(instance.Path.rel(), rawPath)

	if info := e.codegen.lookup(output); info != nil {
		return ResolvedResource{
			Input:           output,
			ProducerRef:     info.ProducerRef,
			ProducerMainOut: info.ProducerMainOut,
			SourceInputs:    info.SourceInputs,
		}
	}

	return ResolvedResource{Input: fallback}
}

type ObjcopyNode struct {
	moduleTag  *string
	kv         *KV
	hashPaths  []string
	keysB64    []string
	kvsHash    []string
	kvsCmd     []string
	pathInputs []VFS
	inputs     InputChunks
	deps       []NodeRef
}

func buildObjcopyNode(ctx *GenCtx, instance ModuleInstance, oc *ObjcopyEmitCtx, n ObjcopyNode) (NodeRef, VFS) {
	na := oc.na
	hash := objcopyHash(n.hashPaths, n.keysB64, n.kvsHash, instance.Path.rel(), n.moduleTag)
	outputObj := build(instance.Path.rel(), "/objcopy_", hash, ".o")
	payload := make([]STR, 0, 2+len(n.pathInputs)+len(n.keysB64)+1+len(n.kvsCmd))

	if len(n.hashPaths) > 0 {
		payload = append(payload, argInputs.str())

		for _, p := range n.pathInputs {
			payload = append(payload, (p).str())
		}

		payload = append(payload, argKeys.str())
		payload = appendInternStrs(payload, n.keysB64)
	}

	if len(n.kvsCmd) > 0 {
		payload = append(payload, argKvs.str())
		payload = appendInternStrs(payload, n.kvsCmd)
	}

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := &Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: objcopyCmdArgs(oc, outputObj, payload), Env: env}),
		Env:          env,
		Inputs:       n.inputs,
		Outputs:      na.vfsList(outputObj),
		KV:           n.kv,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    instance.Platform.UsesPython3Clang,
		DepRefs:      n.deps,
	}

	return ctx.emit.emit(node), outputObj
}

type ObjcopyLayout int

type ObjcopyProfile struct {
	moduleTag   *string
	kv          *KV
	layout      ObjcopyLayout
	resolveDeps bool
}

type objcopyAcc struct {
	paths         []string
	keysB64       []string
	kvsHash       []string
	kvsCmd        []string
	pathInputs    []VFS
	inps          []VFS
	kvInputs      []VFS
	closureInputs []VFS
	srcAttrInputs []VFS
	mainOuts      []VFS
	deps          []NodeRef
	cmdLen        int
}

type ObjcopyBatcher struct {
	e       *EmitContext
	oc      *ObjcopyEmitCtx
	profile ObjcopyProfile
	depSeen map[NodeRef]struct{}
	cur     objcopyAcc
	refs    []NodeRef
	outs    []VFS
}

func newObjcopyBatcher(e *EmitContext, oc *ObjcopyEmitCtx, profile ObjcopyProfile) *ObjcopyBatcher {
	batchDeduper.reset()

	return &ObjcopyBatcher{e: e, oc: oc, profile: profile}
}

func (b *ObjcopyBatcher) results() ([]NodeRef, []VFS) {
	return b.refs, b.outs
}

func (b *ObjcopyBatcher) addInput(v VFS) {
	if batchDeduper.add(v) {
		b.cur.inps = append(b.cur.inps, v)
	}
}

func (b *ObjcopyBatcher) kvEntry(kvHash, kvCmd string, input VFS, extra []VFS) {
	b.cur.kvsHash = append(b.cur.kvsHash, kvHash)
	b.cur.kvsCmd = append(b.cur.kvsCmd, kvCmd)

	b.addInput(input)

	for _, v := range extra {
		b.addInput(v)
	}

	b.cur.cmdLen += rootCmdLen + len(kvHash)

	b.maybeFlush()
}

func (b *ObjcopyBatcher) fileEntry(pathHash, key string, input VFS, extra []VFS) {
	kb64 := encb64.StdEncoding.EncodeToString([]byte(key))

	b.cur.paths = append(b.cur.paths, pathHash)
	b.cur.keysB64 = append(b.cur.keysB64, kb64)
	b.cur.pathInputs = append(b.cur.pathInputs, input)

	b.addInput(input)

	for _, v := range extra {
		b.addInput(v)
	}

	b.cur.cmdLen += rootCmdLen + len(pathHash) + len(kb64)

	b.maybeFlush()
}

func (b *ObjcopyBatcher) genProtoEntry(token, key string, output VFS, producer NodeRef) {
	kvHash := "resfs/src/" + key + "=${rootrel;context=TEXT;input=TEXT:\"" + token + "\"}"
	kvCmd := "resfs/src/" + key + "=" + output.rel()
	kb64 := encb64.StdEncoding.EncodeToString([]byte(key))

	b.cur.paths = append(b.cur.paths, token)
	b.cur.keysB64 = append(b.cur.keysB64, kb64)
	b.cur.kvsHash = append(b.cur.kvsHash, kvHash)
	b.cur.kvsCmd = append(b.cur.kvsCmd, kvCmd)
	b.cur.pathInputs = append(b.cur.pathInputs, output)
	b.cur.inps = append(b.cur.inps, output)

	if producer != NodeRef(0) {
		if b.depSeen == nil {
			b.depSeen = map[NodeRef]struct{}{}
		}

		if _, ok := b.depSeen[producer]; !ok {
			b.depSeen[producer] = struct{}{}
			b.cur.deps = append(b.cur.deps, producer)
		}
	}

	b.cur.cmdLen += rootCmdLen + len(token) + len(kb64) + len(kvHash) + len(kvCmd)

	b.maybeFlush()
}

func (b *ObjcopyBatcher) resourceKvEntry(key string) {
	e := b.e

	b.cur.kvsHash = append(b.cur.kvsHash, key)

	if inner, ok := rootrelInputPath(key); ok {
		r := e.resolveResourceInput(inner, copyFileInputVFS(e.ctx.fs, e.instance.Path, inner))

		b.cur.kvInputs = append(b.cur.kvInputs, r.Input)
		b.cur.mainOuts = append(b.cur.mainOuts, r.ProducerMainOut)
		b.cur.kvsCmd = append(b.cur.kvsCmd, renderResourceKvCmd(rootrelExpand(key, r.Input.rel())))
	} else {
		b.cur.kvsCmd = append(b.cur.kvsCmd, renderResourceKvCmd(key))
	}

	b.cur.cmdLen += rootCmdLen + len(key)
}

func (b *ObjcopyBatcher) resourceFileEntry(path, key string) {
	e := b.e
	r := e.resolveResourceInput(path, copyFileInputVFS(e.ctx.fs, e.instance.Path, path))

	b.cur.paths = append(b.cur.paths, path)
	b.cur.pathInputs = append(b.cur.pathInputs, r.Input)
	b.cur.mainOuts = append(b.cur.mainOuts, r.ProducerMainOut)

	if r.ProducerRef != 0 {
		for _, v := range walkClosureTail(e.scanner, r.Input, e.d.cc.ScanCfg) {
			if v.isBuild() {
				b.cur.closureInputs = append(b.cur.closureInputs, v)
			}
		}

		for _, v := range r.SourceInputs {
			if v.isSource() && objcopySourceLeafKept(v.rel()) {
				b.cur.srcAttrInputs = append(b.cur.srcAttrInputs, v)
			}
		}
	}

	kb := encb64.StdEncoding.EncodeToString([]byte(key))

	b.cur.keysB64 = append(b.cur.keysB64, kb)
	b.cur.cmdLen += rootCmdLen + len(path) + len(kb)
}

func (b *ObjcopyBatcher) entryDone(endsBatch bool) {
	if b.cur.cmdLen > maxCmdLen || endsBatch {
		b.flush()
	}
}

func (b *ObjcopyBatcher) maybeFlush() {
	if b.cur.cmdLen >= maxCmdLen {
		b.flush()
	}
}

func (b *ObjcopyBatcher) flush() {
	if b.cur.cmdLen == 0 {
		return
	}

	e, oc := b.e, b.oc
	ctx, instance := e.ctx, e.instance
	na := ctx.na

	var inputs InputChunks

	switch b.profile.layout {
	case objcopyLayoutResource:
		if len(b.cur.paths) <= 1 {
			inputs = na.inputList(rescompilersWithScriptChunk, b.cur.pathInputs)
		} else {
			inputs = na.inputList(rescompilersChunk, b.cur.pathInputs, objcopyScriptChunk)
		}

		deduper.reset()

		for _, ch := range inputs {
			for _, p := range ch {
				deduper.add(p)
			}
		}

		var tail []VFS

		for _, p := range b.cur.closureInputs {
			if deduper.add(p) {
				tail = append(tail, p)
			}
		}

		for _, p := range b.cur.kvInputs {
			if deduper.add(p) {
				tail = append(tail, p)
			}
		}

		for _, p := range b.cur.srcAttrInputs {
			if deduper.add(p) {
				tail = append(tail, p)
			}
		}

		if len(tail) > 0 {
			inputs = append(inputs, tail)
		}

		var mainTail []VFS

		for _, p := range b.cur.mainOuts {
			if p == 0 {
				continue
			}

			if deduper.add(p) {
				mainTail = append(mainTail, p)
			}
		}

		if len(mainTail) > 0 {
			inputs = append(inputs, mainTail)
		}
	case objcopyLayoutScriptTail:
		inputs = na.inputList(rescompilersChunk, b.cur.inps, objcopyScriptChunk)
	}

	var deps []NodeRef

	if b.profile.resolveDeps {
		var dataInputs []VFS

		switch b.profile.layout {
		case objcopyLayoutResource:
			dataInputs = make([]VFS, 0, len(b.cur.pathInputs)+len(b.cur.closureInputs)+len(b.cur.kvInputs))
			dataInputs = append(dataInputs, b.cur.pathInputs...)
			dataInputs = append(dataInputs, b.cur.closureInputs...)
			dataInputs = append(dataInputs, b.cur.kvInputs...)
		case objcopyLayoutScriptTail:
			dataInputs = b.cur.inps
		}

		deps = resolveCodegenDepRefsIncl(ctx, instance, na, dataInputs, depRefs(oc.rescompilerLDRef, oc.rescompressorLDRef)...)
	} else {
		deps = concat(depRefs(oc.rescompilerLDRef, oc.rescompressorLDRef), b.cur.deps)
	}

	r, outputObj := buildObjcopyNode(ctx, instance, oc, ObjcopyNode{
		moduleTag:  b.profile.moduleTag,
		kv:         b.profile.kv,
		hashPaths:  b.cur.paths,
		keysB64:    b.cur.keysB64,
		kvsHash:    b.cur.kvsHash,
		kvsCmd:     b.cur.kvsCmd,
		pathInputs: b.cur.pathInputs,
		inputs:     inputs,
		deps:       deps,
	})

	b.refs = append(b.refs, r)
	b.outs = append(b.outs, outputObj)
	b.cur = objcopyAcc{}
	b.depSeen = nil

	batchDeduper.reset()
}

type AuxChunk struct {
	hashInputs []string
	cmdArgs    []string
	inputs     []VFS
	deps       []NodeRef
}

func chunkAuxEntries(entries []RawAuxEntry) []AuxChunk {
	var chunks []AuxChunk

	cur := AuxChunk{}
	cmdLen := 0

	deduper.reset()

	depSeen := map[NodeRef]struct{}{}

	addInput := func(v VFS) {
		if !deduper.add(v) {
			return
		}

		cur.inputs = append(cur.inputs, v)
	}

	addDep := func(ref NodeRef) {
		if ref == (NodeRef(0)) {
			return
		}

		if _, ok := depSeen[ref]; ok {
			return
		}

		depSeen[ref] = struct{}{}
		cur.deps = append(cur.deps, ref)
	}

	flush := func() {
		if cmdLen == 0 {
			return
		}

		chunks = append(chunks, cur)
		cur = AuxChunk{}
		cmdLen = 0
		deduper.reset()
		depSeen = map[NodeRef]struct{}{}
	}

	for _, e := range entries {
		key := "resfs/file/py/" + e.key
		arcBuildPath := "${ARCADIA_BUILD_ROOT}/" + e.path.rel()
		kvHash := "resfs/src/" + key + "=${rootrel;context=TEXT;input=TEXT:\"" + arcBuildPath + "\"}"
		kvCmd := "resfs/src/" + key + "=" + e.path.rel()

		cur.hashInputs = append(cur.hashInputs, "-", kvHash)
		cur.cmdArgs = append(cur.cmdArgs, "-", kvCmd)
		addInput(e.path)

		for _, input := range e.inputs {
			addInput(input)
		}

		addDep(e.producer)
		cmdLen += rootCmdLen + len("-") + len(kvHash)

		if cmdLen >= maxCmdLen {
			flush()
		}

		cur.hashInputs = append(cur.hashInputs, arcBuildPath, "-"+key)
		cur.cmdArgs = append(cur.cmdArgs, e.path.string(), "-"+key)
		addInput(e.path)

		for _, input := range e.inputs {
			addInput(input)
		}

		addDep(e.producer)
		cmdLen += rootCmdLen + len(arcBuildPath) + len(key)

		if cmdLen >= maxCmdLen {
			flush()
		}
	}

	flush()

	return chunks
}

type RawAuxResult struct {
	Refs    []NodeRef
	Outputs []VFS
}

func (e *EmitContext) emitRawAuxChunks(entries []RawAuxEntry, hashTag string, resolveDeps bool, closure func(aux VFS, inputs []VFS, ref NodeRef) []VFS) *RawAuxResult {
	ctx, instance, _ := e.ctx, e.instance, e.d
	na := ctx.na

	if len(entries) == 0 {
		return nil
	}

	rescompilerRef, _ := ctx.tool(argToolsRescompiler)
	res := &RawAuxResult{}

	for _, ch := range chunkAuxEntries(entries) {
		aux := build(instance.Path.rel(), "/", resourcePackHash(ch.hashInputs, "$S/"+instance.Path.rel(), hashTag), "_raw.auxcpp")
		auxRef := ctx.emit.reserve()
		auxClosure := closure(aux, ch.inputs, auxRef)
		cmdArgs := []STR{internStr(rescompilerBinPath), (aux).str()}

		cmdArgs = appendInternStrs(cmdArgs, ch.cmdArgs)

		deps := concat(ch.deps, depRefs(rescompilerRef))

		if resolveDeps {
			deps = resolveCodegenDepRefsIncl(ctx, instance, na, ch.inputs, deps...)
		}

		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

		deduper.reset()

		for _, p := range ch.inputs {
			deduper.add(p)
		}

		tail := make([]VFS, 0, 1+len(auxClosure))

		if deduper.add(rescompilerBinVFS) {
			tail = append(tail, rescompilerBinVFS)
		}

		for _, p := range auxClosure {
			if p == aux {
				continue
			}

			if deduper.add(p) {
				tail = append(tail, p)
			}
		}

		ctx.emit.emitReserved(&Node{
			Platform:     instance.Platform,
			Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
			Env:          env,
			Inputs:       na.inputList(ch.inputs, tail),
			Outputs:      na.vfsList(aux),
			KV:           &rawAuxKV,
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			DepRefs:      deps,
		}, auxRef)

		res.Refs = append(res.Refs, auxRef)
		res.Outputs = append(res.Outputs, aux)
	}

	return res
}

type ObjcopyEmitResult struct {
	Refs            []NodeRef
	Outputs         []VFS
	PySrcTrailCount int
}

type ObjcopyEmit struct {
	Ref NodeRef
	Out VFS
}

func (e *EmitContext) emitKvOnlyResource(tag *string, kvsHash, kvsCmd []string) *ObjcopyEmit {
	items := make([]ResourceItem, len(kvsHash))

	for i := range kvsHash {
		items[i] = ResourceItem{Path: "-", Key: kvsHash[i], Cmd: kvsCmd[i]}
	}

	refs, outs := e.packResources(ResourcePack{Tag: tag, Items: items})

	return &ObjcopyEmit{Ref: refs[0], Out: outs[0]}
}

func (e *EmitContext) emitResourceFile(entries []ResourceEntry, moduleTag *string) (refs []NodeRef, outs []VFS) {
	ctx, instance, d := e.ctx, e.instance, e.d
	batch := make([]ResourceItem, 0, len(entries))

	flushBatch := func() {
		if len(batch) == 0 {
			return
		}

		r, o := e.packResources(ResourcePack{Tag: moduleTag, Items: batch})

		refs = append(refs, r...)
		outs = append(outs, o...)
		batch = batch[:0]
	}

	for _, entry := range entries {
		if entry.Path == "-" {
			it := ResourceItem{Path: "-", Key: entry.Key}

			if inner, ok := rootrelInputPath(entry.Key); ok {
				r := e.resolveResourceInput(inner, copyFileInputVFS(ctx.fs, instance.Path, inner))

				it.Cmd = renderResourceKvCmd(rootrelExpand(entry.Key, r.Input.rel()))
				it.Aux = []VFS{r.Input, r.ProducerMainOut}
			} else {
				it.Cmd = renderResourceKvCmd(entry.Key)
			}

			batch = append(batch, it)
		} else {
			r := e.resolveResourceInput(entry.Path, copyFileInputVFS(ctx.fs, instance.Path, entry.Path))
			it := ResourceItem{Path: entry.Path, Key: entry.Key, Input: r.Input}

			if r.ProducerRef != 0 {
				for _, v := range walkClosureTail(e.scanner, r.Input, d.cc.ScanCfg) {
					if v.isBuild() {
						it.Aux = append(it.Aux, v)
					}
				}

				for _, v := range r.SourceInputs {
					if v.isSource() && objcopySourceLeafKept(v.rel()) {
						it.Aux = append(it.Aux, v)
					}
				}
			}

			it.Aux = append(it.Aux, r.ProducerMainOut)
			batch = append(batch, it)
		}

		if entry.EndsBatch {
			flushBatch()
		}
	}

	flushBatch()

	return refs, outs
}

type RawAuxEntry struct {
	path     VFS
	key      string
	producer NodeRef
	inputs   []VFS
}

type ResourceItem struct {
	Path  string
	Key   string
	Cmd   string
	Input VFS
	Extra []VFS
	Aux   []VFS
}

type ResourcePack struct {
	Tag        *string
	Items      []ResourceItem
	RawClosure func(aux VFS, inputs []VFS, ref NodeRef) []VFS
}

type resourceChunk struct {
	paths     []string
	keysB64   []string
	kvsHash   []string
	kvsCmd    []string
	hashPairs []string
	cmdPairs  []string
	payload   []VFS
	adjacent  []VFS
	aux       []VFS
	cmdLen    int
}

func splitResourceChunks(items []ResourceItem, objcopy bool) []resourceChunk {
	var chunks []resourceChunk

	cur := resourceChunk{}
	seen := map[VFS]struct{}{}

	addAdjacent := func(v VFS) {
		if v == 0 {
			return
		}

		if _, dup := seen[v]; dup {
			return
		}

		seen[v] = struct{}{}
		cur.adjacent = append(cur.adjacent, v)
	}

	flush := func() {
		if cur.cmdLen == 0 {
			return
		}

		chunks = append(chunks, cur)
		cur = resourceChunk{}
		seen = map[VFS]struct{}{}
	}

	for _, it := range items {
		if it.Path == "-" {
			cur.kvsHash = append(cur.kvsHash, it.Key)
			cur.kvsCmd = append(cur.kvsCmd, it.Cmd)
			cur.hashPairs = append(cur.hashPairs, "-", it.Key)
			cur.cmdPairs = append(cur.cmdPairs, "-", it.Cmd)
			cur.cmdLen += rootCmdLen + len(it.Key)
		} else {
			cur.paths = append(cur.paths, it.Path)

			key := it.Key

			if objcopy {
				key = encb64.StdEncoding.EncodeToString([]byte(it.Key))
			}

			cur.keysB64 = append(cur.keysB64, key)
			cur.hashPairs = append(cur.hashPairs, it.Path, "-"+it.Key)
			cur.cmdPairs = append(cur.cmdPairs, it.Input.string(), "-"+it.Key)
			cur.payload = append(cur.payload, it.Input)
			cur.cmdLen += rootCmdLen + len(it.Path) + len(key)
		}

		addAdjacent(it.Input)

		for _, v := range it.Extra {
			addAdjacent(v)
		}

		cur.aux = append(cur.aux, it.Aux...)

		if cur.cmdLen >= maxCmdLen {
			flush()
		}
	}

	flush()

	return chunks
}

func (e *EmitContext) packResources(p ResourcePack) (refs []NodeRef, outs []VFS) {
	var objItems, rawItems []ResourceItem

	for _, it := range p.Items {
		if resourceCanObjcopy(it.Path, it.Key) {
			objItems = append(objItems, it)
		} else {
			rawItems = append(rawItems, it)
		}
	}

	if len(rawItems) > 0 {
		r, o := e.packRawResourceChunks(rawItems, p)

		refs = append(refs, r...)
		outs = append(outs, o...)
	}

	if len(objItems) > 0 {
		r, o := e.packObjcopyResourceChunks(objItems, p)

		refs = append(refs, r...)
		outs = append(outs, o...)
	}

	return refs, outs
}

func (e *EmitContext) packObjcopyResourceChunks(items []ResourceItem, p ResourcePack) (refs []NodeRef, outs []VFS) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	oc := newObjcopyEmitCtx(ctx, d, instance.Platform)

	for _, ch := range splitResourceChunks(items, true) {
		deduper.reset()

		for _, v := range rescompilersWithScriptChunk {
			deduper.add(v)
		}

		for _, v := range ch.adjacent {
			deduper.add(v)
		}

		var tail []VFS

		for _, v := range ch.aux {
			if v == 0 {
				continue
			}

			if deduper.add(v) {
				tail = append(tail, v)
			}
		}

		inputs := na.inputList(rescompilersChunk, ch.adjacent, objcopyScriptChunk)

		if len(tail) > 0 {
			inputs = append(inputs, tail)
		}

		dataInputs := concat(ch.adjacent, tail)

		r, outputObj := buildObjcopyNode(ctx, instance, oc, ObjcopyNode{
			moduleTag:  p.Tag,
			kv:         &pyObjcopyKV,
			hashPaths:  ch.paths,
			keysB64:    ch.keysB64,
			kvsHash:    ch.kvsHash,
			kvsCmd:     ch.kvsCmd,
			pathInputs: ch.payload,
			inputs:     inputs,
			deps:       resolveCodegenDepRefsIncl(ctx, instance, na, dataInputs, depRefs(oc.rescompilerLDRef, oc.rescompressorLDRef)...),
		})

		refs = append(refs, r)
		outs = append(outs, outputObj)
	}

	return refs, outs
}

func (e *EmitContext) packRawResourceChunks(items []ResourceItem, p ResourcePack) (refs []NodeRef, outs []VFS) {
	ctx, instance := e.ctx, e.instance
	na := ctx.na

	if p.RawClosure == nil {
		throwFmt("packResources: %s has raw-routed resource items but no RawClosure", instance.Path.rel())
	}

	rescompilerRef, _ := ctx.tool(argToolsRescompiler)
	tag := ""

	if p.Tag != nil {
		tag = *p.Tag
	}

	for _, ch := range splitResourceChunks(items, false) {
		aux := build(instance.Path.rel(), "/", resourcePackHash(ch.hashPairs, "$S/"+instance.Path.rel(), tag), "_raw.auxcpp")
		auxRef := ctx.emit.reserve()
		auxClosure := p.RawClosure(aux, ch.adjacent, auxRef)
		nodeCmd := []STR{internStr(rescompilerBinPath), (aux).str()}

		nodeCmd = appendInternStrs(nodeCmd, ch.cmdPairs)

		deps := append(resolveCodegenDepRefsIncl(ctx, instance, na, ch.adjacent), depRefs(rescompilerRef)...)
		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

		deduper.reset()

		for _, v := range ch.adjacent {
			deduper.add(v)
		}

		tail := make([]VFS, 0, 1+len(auxClosure))

		if deduper.add(rescompilerBinVFS) {
			tail = append(tail, rescompilerBinVFS)
		}

		for _, v := range auxClosure {
			if v == aux {
				continue
			}

			if deduper.add(v) {
				tail = append(tail, v)
			}
		}

		ctx.emit.emitReserved(&Node{
			Platform:     instance.Platform,
			Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(nodeCmd), Env: env}),
			Env:          env,
			Inputs:       na.inputList(ch.adjacent, tail),
			Outputs:      na.vfsList(aux),
			KV:           &rawAuxKV,
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			DepRefs:      deps,
		}, auxRef)

		refs = append(refs, auxRef)
		outs = append(outs, aux)
	}

	return refs, outs
}
