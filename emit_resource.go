package main

import (
	"bytes"
	"crypto/md5"
	encb64 "encoding/base64"
	enchex "encoding/hex"
	"sort"
	"strings"
)

var (
	rescompilersChunk           = []VFS{rescompilerBinVFS, rescompressorBinVFS}
	rescompilersWithScriptChunk = []VFS{rescompilerBinVFS, rescompressorBinVFS, objcopyScriptVFS}
	objcopyScriptChunk          = []VFS{objcopyScriptVFS}
	rawAuxKV                    = KV{P: pkPR, PC: pcYellow, ShowOut: true}
	pyObjcopyKV                 = KV{P: pkPY, PC: pcYellow, ShowOut: true}
	resKvMacroPrefix            = []byte("${")
	resourceCxxFlags            = []ANY{argX.any(), argC.any()}
)

const (
	hashLen    = 26
	rootCmdLen = 200
	maxCmdLen  = 8000
)

func resourceCanObjcopy(path, key string) bool {
	for _, bad := range []string{"${ARCADIA_BUILD_ROOT}", "${ARCADIA_SOURCE_ROOT}", "conftest.py"} {
		if strings.Contains(path, bad) || strings.Contains(key, bad) {
			return false
		}
	}

	return true
}

func resourceHashInto(buf []byte, list []string, moduleTag string) (string, []byte) {
	if len(list) > 1 {
		sort.Strings(list)
	}

	buf = buf[:0]

	for i, s := range list {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = append(buf, s...)
	}

	buf = append(buf, moduleTag...)

	sum := md5.Sum(buf)
	mark := len(buf)

	buf = append(buf, make([]byte, 2*md5.Size)...)
	enchex.Encode(buf[mark:], sum[:])

	return bytesString(buf[mark : mark+hashLen]), buf
}

func renderResourceKvCmd(kv, modulePath string) string {
	kv = strings.ReplaceAll(kv, "${ARCADIA_ROOT}/", "$(S)/")
	kv = strings.ReplaceAll(kv, "${ARCADIA_BUILD_ROOT}/", "$(B)/")
	kv = strings.ReplaceAll(kv, "${CURDIR}", "$(S)/"+modulePath)
	kv = strings.ReplaceAll(kv, "${BINDIR}", "$(B)/"+modulePath)

	return kv
}

func rootrelSplit(kv string) (head, input, tail string, ok bool) {
	const marker = "${rootrel;context=TEXT;input=TEXT:\""

	idx := strings.Index(kv, marker)

	if idx < 0 {
		return "", "", "", false
	}

	rest := kv[idx+len(marker):]
	end := strings.Index(rest, "\"}")

	if end < 0 {
		return "", "", "", false
	}

	return kv[:idx], rest[:end], rest[end+len("\"}"):], true
}

type ObjcopyArgBlocks struct {
	pre  []ANY
	post []ANY
}

type ObjcopyEmitCtx struct {
	unitElem           STR
	deps               []NodeRef
	rescompilerLDRef   NodeRef
	rescompressorLDRef NodeRef
	blocks             ObjcopyArgBlocks
	na                 *NodeArenas
}

func (e *EmitContext) objcopyEmitCtx() *ObjcopyEmitCtx {
	if e.objcopyOk {
		return &e.objcopyCtx
	}

	e.objcopyOk = true

	ctx, d := e.ctx, e.d
	oc := &e.objcopyCtx

	*oc = ObjcopyEmitCtx{na: ctx.na}
	oc.rescompilerLDRef, _ = e.tool(argToolsRescompiler)
	oc.rescompressorLDRef, _ = e.tool(argToolsRescompressor)
	oc.unitElem = internJoined("$S", cleanRel(e.instance.Path.relString()))
	oc.deps = ctx.na.refList(oc.rescompilerLDRef, oc.rescompressorLDRef)
	oc.blocks = composeObjcopyArgBlocks(ctx.na, d.tc, instancePlatform(e), Environment{s: &e.frame.ifEnv}.string(envYmakeObjcopyLargeResourceThr))

	return oc
}

func instancePlatform(e *EmitContext) *Platform {
	return e.instance.Platform
}

func composeObjcopyArgBlocks(na *NodeArenas, tc ModuleToolchain, p *Platform, largeResourceThr string) ObjcopyArgBlocks {
	post := []ANY{argTarget.any(), internStr(p.Triple).any()}

	if largeResourceThr != "" {
		post = append(post, argLargeResourceThr.any(), internStr(largeResourceThr).any())
	}

	return ObjcopyArgBlocks{
		pre: na.anyList(
			tc.Python3,
			objcopyScriptVFS.any(),
			argCompiler.any(), tc.CXX.any(),
			argObjcopy.any(), tc.Objcopy.any(),
			argCompressor.any(), rescompressorBinVFS.any(),
			argRescompiler.any(), rescompilerBinVFS.any(),
			argOutputObj.any(),
		),
		post: na.anyList(post...),
	}
}

func objcopyCmdArgs(oc *ObjcopyEmitCtx, outputObj VFS, payload []ANY) ArgChunks {
	return oc.na.chunkList(oc.blocks.pre, oc.na.anyList(outputObj.any()), oc.blocks.post, payload)
}

type ResolvedResource struct {
	Input           VFS
	ProducerRef     NodeRef
	ProducerMainOut VFS
}

func (e *EmitContext) resolveResourceInput(rawPath string, fallback VFS) ResolvedResource {
	_, instance := e.ctx, e.instance
	output := resourceOutputVFS(instance.Path.relString(), rawPath)

	if fallback.isBuild() {
		output = fallback
	}

	if info := e.codegen.use(output); info != nil {
		return ResolvedResource{
			Input:           output,
			ProducerRef:     info.ProducerRef,
			ProducerMainOut: info.ProducerMainOut,
		}
	}

	return ResolvedResource{Input: fallback}
}

func (e *EmitContext) resourceSourceMeta(items []ResourceItem, output VFS) SrcMeta {
	if e.ctx.testMode {
		for _, item := range items {
			e.collectCPPStyleInputs(item.input())
		}
	}

	return SrcMeta{Source: output.any(), Global: true}
}

func (e *EmitContext) buildObjcopyNode(oc *ObjcopyEmitCtx, kv *KV, outputObj VFS, payload []ANY, resources []STR, inputs, buildFrom InputChunks, textInputs []VFS, deps []NodeRef) NodeRef {
	if info := e.codegen.lookup(outputObj); info != nil && info.ResourceObjcopy && info.OwnerModule == e.instance.Path && info.OwnerSerial != e.frame.serial {
		return info.ProducerRef
	}

	na := oc.na
	env := envVarsVCS

	node := NodeSpec{Node: Node{Platform: e.instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: objcopyCmdArgs(oc, outputObj, payload), Resources: resources, Env: env}),
		Env:  env, Inputs: inputs, Outputs: na.vfsList(outputObj),
		KV:        kv,
		Resources: e.instance.Platform.UsesPython3Clang,
		DepRefs:   deps}, input: InputSpec{roots: buildFrom, text: textInputs},
	}

	ref := e.emitNode(node)

	e.register(GeneratedFileSpec{OutputPath: outputObj, ProducerRef: ref, ResourceObjcopy: true})

	return ref
}

type ResourceItem struct {
	DontParse   bool
	Path        string
	Key         string
	Cmd         STR
	ExpandedCmd CompilerFlagValue
	SourceInput VFS
	BuildInput  VFS
	AuxBuilds   []VFS
}

func (it *ResourceItem) commandLen() int {
	if it.ExpandedCmd.Args != nil {
		return len(it.ExpandedCmd.Args)
	}

	return 1
}

func (it *ResourceItem) appendCommand(dst []ANY) []ANY {
	if it.ExpandedCmd.Args != nil {
		return append(dst, it.ExpandedCmd.Args...)
	}

	return append(dst, it.Cmd.any())
}

func (it ResourceItem) input() VFS {
	if it.BuildInput != 0 {
		return it.BuildInput
	}

	return it.SourceInput
}

type ResourcePack struct {
	Tag          STR
	Items        []ResourceItem
	RawCompile   CompileSpec
	DontCompress bool
}

func (it *ResourceItem) setInput(v VFS) {
	if v.isBuild() {
		it.BuildInput = v
	} else {
		it.SourceInput = v
	}
}

func (e *EmitContext) resourceTextInputs(items []ResourceItem) []VFS {
	inputs := e.ctx.na.vfs.alloc(len(items))
	k := 0

	dedupers.with(func(dd *DeDuper) {
		for _, item := range items {
			if !item.DontParse {
				dd.addStable(item.input().strID())
			}
		}

		for _, item := range items {
			input := item.input()

			if item.DontParse && input != 0 && input.isSource() && dd.addStable(input.strID()) {
				inputs[k] = input
				k++
			}
		}
	})

	e.ctx.na.vfs.commit(k)

	return inputs[:k:k]
}

func resourceItemCmdLen(it ResourceItem, objcopy bool) int {
	if objcopy {
		if it.Path == "-" {
			return rootCmdLen + len(it.Key)
		}

		return rootCmdLen + len(it.Path) + encb64.StdEncoding.EncodedLen(len(it.Key))
	}

	return rootCmdLen + len(it.Path) + len(it.Key)
}

func resourceChunkEnd(items []ResourceItem) int {
	cmdLen := 0

	for i := range items {
		cmdLen += resourceItemCmdLen(items[i], false)

		if cmdLen >= maxCmdLen {
			return i + 1
		}
	}

	return len(items)
}

type ResourceChunkSize struct {
	paths, kvs, args, sources, builds int
}

func (s *ResourceChunkSize) add(it *ResourceItem) {
	if it.Path == "-" {
		s.kvs++
		s.args += it.commandLen()
	} else {
		s.paths++
	}

	if it.SourceInput != 0 {
		s.sources++
	}

	if it.BuildInput != 0 {
		s.builds++
	}

	s.builds += len(it.AuxBuilds)
}

func (e *EmitContext) packResources(p ResourcePack) ([]NodeRef, []VFS) {
	objItems := e.objScratch[:0]
	rawItems := e.rawScratch[:0]
	refs, outs := nodeRefScratches.get(), vfsScratches.get()

	defer func() {
		nodeRefScratches.put(refs)
		vfsScratches.put(outs)
	}()

	var objSize ResourceChunkSize

	defer func() {
		e.objScratch = retainMaxLen(e.objScratch, objItems)
		e.rawScratch = retainMaxLen(e.rawScratch, rawItems)
	}()

	flushObj := func() {
		if len(objItems) == 0 {
			return
		}

		r, o := e.packObjcopyResourceChunk(objItems, p, objSize)

		refs = append(refs, r)
		outs = append(outs, o)
		e.objScratch = retainMaxLen(e.objScratch, objItems)
		objItems = objItems[:0]
		objSize = ResourceChunkSize{}
	}

	useYasm := e.instance.Platform.ISA == ISAX8664 && !p.DontCompress && !p.RawCompile.ForceCxx

	flushRaw := func() {
		if len(rawItems) == 0 {
			return
		}

		if useYasm {
			r, o := e.packYasmResourceChunks(rawItems, p)

			refs = append(refs, r...)
			outs = append(outs, o...)
		} else {
			if !p.RawCompile.ForceCxx {
				p.RawCompile = CompileSpec{ForceCxx: true, CFlags: e.instance.Platform.ForceCppFlags}
			}

			ref, out := e.packRawResourceChunk(rawItems, p)

			refs = append(refs, ref)
			outs = append(outs, out)
		}

		e.rawScratch = retainMaxLen(e.rawScratch, rawItems)
		rawItems = rawItems[:0]
	}

	objLen, rawLen := 0, 0

	for _, it := range p.Items {
		if it.Path == "-" && it.ExpandedCmd.Args == nil && strings.ContainsAny(it.Cmd.string(), "\"'\\") {
			it.ExpandedCmd.Args = e.ctx.na.anyList(internAnys(splitShellWords(it.Cmd.string()))...)
		}

		if e.instance.Platform.UseObjcopy && resourceCanObjcopy(it.Path, it.Key) {
			objItems = append(objItems, it)
			objSize.add(&it)
			objLen += resourceItemCmdLen(it, true)

			if objLen >= maxCmdLen {
				flushObj()
				objLen = 0
			}
		} else {
			rawItems = append(rawItems, it)
			rawLen += resourceItemCmdLen(it, false)

			if rawLen >= maxCmdLen && !useYasm {
				flushRaw()
				rawLen = 0
			}
		}
	}

	flushObj()
	flushRaw()

	return e.ctx.na.refList(refs...), e.ctx.na.vfsList(outs...)
}

func (e *EmitContext) packObjcopyResourceChunk(chunk []ResourceItem, p ResourcePack, size ResourceChunkSize) (NodeRef, VFS) {
	ctx, instance := e.ctx, e.instance
	na := ctx.na
	oc := e.objcopyEmitCtx()
	unitElem := oc.unitElem.string()
	tag := ""

	if p.Tag != 0 {
		tag = p.Tag.string()
	}

	hashScratch := ctx.resHashScratch[:0]
	hashBuf := ctx.resHashBuf
	b64Scratch := ctx.resB64Scratch

	defer func() {
		ctx.resHashScratch = hashScratch[:0]
		ctx.resHashBuf = hashBuf
		ctx.resB64Scratch = b64Scratch
	}()

	nPaths, nKvs, nKvArgs := size.paths, size.kvs, size.args
	sourceBound, buildBound := size.sources, size.builds
	payloadCap := 0

	if nPaths > 0 {
		payloadCap += 2 + 2*nPaths
	}

	if nKvs > 0 {
		payloadCap += 1 + nKvArgs
	}

	payload := na.anys.alloc(payloadCap)
	paths, keys, kvs := 0, 0, 0

	if nPaths > 0 {
		payload[0] = argInputs.any()
		paths, keys, kvs = 1, nPaths+2, 2*nPaths+2
		payload[nPaths+1] = argKeys.any()
	}

	if nKvs > 0 {
		payload[kvs] = argKvs.any()
		kvs++
	}

	var resourceUses []STR
	hashScratch = hashScratch[:0]

	for i := range chunk {
		it := &chunk[i]

		if it.Path == "-" {
			args := it.ExpandedCmd.Args

			if args == nil {
				payload[kvs] = it.Cmd.any()
				kvs++
			} else {
				kvs += copy(payload[kvs:], args)
			}

			resourceUses = resourceKeyUnion(resourceUses, it.ExpandedCmd.Resources)
			hashScratch = append(hashScratch, it.Key)
		} else {
			payload[paths] = it.input().any()
			paths++
			b64Scratch = encb64.StdEncoding.AppendEncode(b64Scratch[:0], strBytes(it.Key))

			key := internBytes(b64Scratch)

			payload[keys] = key.any()
			keys++
			hashScratch = append(hashScratch, it.Path, key.string())
		}
	}

	na.anys.commit(payloadCap)
	payload = payload[:payloadCap:payloadCap]

	var adjacentSources, adjacentBuilds, tailBuilds []VFS

	dedupers.with(func(deduper *DeDuper) {
		adjacentSources = na.vfs.alloc(sourceBound)[:0]

		for i := range chunk {
			it := &chunk[i]

			if it.SourceInput != 0 && deduper.add(it.SourceInput.strID()) {
				adjacentSources = append(adjacentSources, it.SourceInput)
			}
		}

		na.vfs.commit(len(adjacentSources))
		adjacentSources = adjacentSources[:len(adjacentSources):len(adjacentSources)]
		adjacentBuilds = na.vfs.alloc(buildBound)[:0]

		for i := range chunk {
			it := &chunk[i]

			if it.BuildInput != 0 && deduper.add(it.BuildInput.strID()) {
				adjacentBuilds = append(adjacentBuilds, it.BuildInput)
			}
		}

		na.vfs.commit(len(adjacentBuilds))
		adjacentBuilds = adjacentBuilds[:len(adjacentBuilds):len(adjacentBuilds)]

		for _, v := range rescompilersWithScriptChunk {
			deduper.add(v.strID())
		}

		tailBuilds = na.vfs.alloc(buildBound)[:0]

		for i := range chunk {
			it := &chunk[i]

			for _, v := range it.AuxBuilds {
				if v != 0 && deduper.add(v.strID()) {
					tailBuilds = append(tailBuilds, v)
				}
			}
		}

		na.vfs.commit(len(tailBuilds))
		tailBuilds = tailBuilds[:len(tailBuilds):len(tailBuilds)]
	})

	inputs := na.inputList(rescompilersChunk, adjacentSources, adjacentBuilds, objcopyScriptChunk, tailBuilds)
	buildFrom := na.inputList(adjacentSources, adjacentBuilds, objcopyScriptChunk)

	hashScratch = append(hashScratch, unitElem)

	var hash string

	hash, hashBuf = resourceHashInto(hashBuf, hashScratch, tag)

	dir, sep := cleanRel(instance.Path.relString()), "/"

	if dir == "" {
		sep = ""
	}

	outputObj := build(dir, sep, "objcopy_", hash, ".o")
	deps := oc.deps
	ref := e.buildObjcopyNode(oc, &pyObjcopyKV, outputObj, payload, resourceUses, inputs, buildFrom, e.resourceTextInputs(chunk), deps)
	meta := e.resourceSourceMeta(chunk, outputObj)

	e.collectObj(ref, outputObj, meta)

	return ref, outputObj
}

func (e *EmitContext) packRawResourceChunk(chunk []ResourceItem, p ResourcePack) (NodeRef, VFS) {
	ctx, instance := e.ctx, e.instance
	na := ctx.na
	rescompilerRef, _ := e.tool(argToolsRescompiler)
	unitElem := internJoined("$S", cleanRel(instance.Path.relString())).string()
	dash := str2
	tag := ""

	if p.Tag != 0 {
		tag = p.Tag.string()
	}

	hashScratch := ctx.resHashScratch[:0]
	hashBuf := ctx.resHashBuf
	dashBuf := ctx.resDashBuf[:0]

	defer func() {
		ctx.resHashScratch = hashScratch[:0]
		ctx.resHashBuf = hashBuf
		ctx.resDashBuf = dashBuf[:0]
	}()

	buildBound, commandWords := 0, 2

	for i := range chunk {
		it := &chunk[i]

		commandWords += 1 + it.commandLen()

		if it.input() != 0 {
			buildBound++
		}
	}

	var payloadBuilds []VFS

	hashScratch = hashScratch[:0]

	for i := range chunk {
		it := &chunk[i]

		if it.Path == "-" {
			hashScratch = append(hashScratch, "-", it.Key)
		} else {
			dashStart := len(dashBuf)

			dashBuf = append(dashBuf, '-')
			dashBuf = append(dashBuf, it.Key...)

			hashScratch = append(hashScratch, it.Path, bytesString(dashBuf[dashStart:]))
		}
	}

	dedupers.with(func(deduper *DeDuper) {
		payloadBuilds = na.vfs.alloc(buildBound)[:0]

		for i := range chunk {
			it := &chunk[i]
			input := it.input()

			if input == 0 || !deduper.add(input.strID()) {
				continue
			}

			payloadBuilds = append(payloadBuilds, input)
		}

		na.vfs.commit(len(payloadBuilds))
		payloadBuilds = payloadBuilds[:len(payloadBuilds):len(payloadBuilds)]
	})

	hashScratch = append(hashScratch, unitElem)

	var hash string

	hash, hashBuf = resourceHashInto(hashBuf, hashScratch, tag)

	aux := build(instance.Path.relString(), "/", hash, "_raw.auxcpp")
	auxRef := ctx.emit.reserve()
	meta := e.resourceSourceMeta(chunk, aux)

	e.register(GeneratedFileSpec{
		OutputPath:    aux,
		ProducerRef:   auxRef,
		GeneratorRefs: na.refList(rescompilerRef),
	})

	nodeCmd := na.anys.alloc(commandWords)[:0]

	nodeCmd = append(nodeCmd, rescompilerBinVFS.any(), aux.any())
	var resourceUses []STR

	for i := range chunk {
		it := &chunk[i]

		if it.Path == "-" {
			nodeCmd = append(nodeCmd, dash.any())
			nodeCmd = it.appendCommand(nodeCmd)

			resourceUses = resourceKeyUnion(resourceUses, it.ExpandedCmd.Resources)
		} else {
			key := it.Key

			if it.Cmd != 0 {
				key = it.Cmd.string()
			}

			nodeCmd = append(nodeCmd, it.input().any(), internV("-", key).any())
		}
	}

	na.anys.commit(len(nodeCmd))

	nodeCmd = nodeCmd[:len(nodeCmd):len(nodeCmd)]

	deps := na.noderefs.alloc(1)
	nd := 0

	if rescompilerRef != 0 {
		deps[nd] = rescompilerRef
		nd++
	}

	na.noderefs.commit(nd)

	deps = deps[:nd:nd]

	env := envVarsVCS

	var tail []VFS

	dedupers.with(func(deduper *DeDuper) {
		for _, v := range payloadBuilds {
			deduper.add(v.strID())
		}

		tail = na.vfs.alloc(1)[:0]

		if deduper.add(rescompilerBinVFS.strID()) {
			tail = append(tail, rescompilerBinVFS)
		}

		na.vfs.commit(len(tail))
	})

	tail = tail[:len(tail):len(tail)]

	e.emitReservedNode(NodeSpec{Node: Node{Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(nodeCmd), Resources: resourceUses, Env: env}),
		Env:  env, Inputs: na.rootInputList(payloadBuilds, tail), Outputs: na.vfsList(aux),
		KV:      &rawAuxKV,
		DepRefs: deps}, input: InputSpec{roots: na.inputList(payloadBuilds), text: e.resourceTextInputs(chunk)},
	}, auxRef)

	ccRef := ctx.emit.reserve()

	meta.Compile, meta.CompileRef = p.RawCompile, ccRef

	e.emitOneSource(meta)

	return ccRef, e.ccOutputFor(aux, p.RawCompile)
}

type ObjcopyEmitResult struct {
	Refs    []NodeRef
	Outputs []VFS
}

func (e *EmitContext) emitKvOnlyResource(tag STR, kvsHash []string, kvsCmd []STR) ([]NodeRef, []VFS) {
	items := make([]ResourceItem, len(kvsHash))

	for i := range kvsHash {
		items[i] = ResourceItem{Path: "-", Key: kvsHash[i], Cmd: kvsCmd[i]}
	}

	return e.packResources(ResourcePack{Tag: tag, Items: items})
}

func (e *EmitContext) emitResourceFile(entries []ResourceEntry, moduleTag STR) (refs []NodeRef, outs []VFS) {
	ctx, instance := e.ctx, e.instance
	na := ctx.na
	batch := e.resItems[:0]
	dontCompress := false

	defer func() { e.resItems = retainMaxLen(e.resItems, batch) }()

	flushBatch := func() {
		if len(batch) == 0 {
			return
		}

		r, o := e.packResources(ResourcePack{Tag: moduleTag, Items: batch, DontCompress: dontCompress})

		refs = append(refs, r...)
		outs = append(outs, o...)
		e.resItems = retainMaxLen(e.resItems, batch)
		batch = batch[:0]
	}

	for _, entry := range entries {
		dontCompress = entry.DontCompress

		if entry.Path == "-" {
			it := ResourceItem{Path: "-", DontParse: true}

			if entry.SrcPath != "" {
				hashStart := len(e.resStrBuf)

				e.resStrBuf = append(e.resStrBuf, "resfs/src/"...)
				e.resStrBuf = append(e.resStrBuf, entry.Key...)
				e.resStrBuf = append(e.resStrBuf, "=${rootrel;context=TEXT;input=TEXT:\""...)
				e.resStrBuf = append(e.resStrBuf, entry.SrcPath...)
				e.resStrBuf = append(e.resStrBuf, "\"}"...)

				it.Key = bytesString(e.resStrBuf[hashStart:])

				r := e.resolveResourceInput(entry.SrcPath, e.resourceSource(entry.SrcPath, entry.Resolved))
				cmdStart := len(e.resStrBuf)

				e.resStrBuf = append(e.resStrBuf, "resfs/src/"...)
				e.resStrBuf = append(e.resStrBuf, entry.Key...)
				e.resStrBuf = append(e.resStrBuf, '=')
				e.resStrBuf = append(e.resStrBuf, r.Input.relString()...)

				cmdView := e.resStrBuf[cmdStart:]

				if bytes.Contains(cmdView, resKvMacroPrefix) {
					it.Cmd = internStr(renderResourceKvCmd(string(cmdView), instance.Path.relString()))
				} else {
					it.Cmd = internBytes(cmdView)
				}

				e.resStrBuf = e.resStrBuf[:cmdStart]
				it.setInput(r.Input)
				it.AuxBuilds = na.vfsList(r.ProducerMainOut)
			} else {
				it.Key = entry.Key

				if head, inner, tail, ok := rootrelSplit(entry.Key); ok {
					r := e.resolveResourceInput(inner, e.resourceSource(inner, entry.Resolved))

					it.Cmd = internStr(renderResourceKvCmd(head+r.Input.relString()+tail, instance.Path.relString()))
					it.setInput(r.Input)
					it.AuxBuilds = na.vfsList(r.ProducerMainOut)
				} else {
					it.Cmd = internStr(renderResourceKvCmd(entry.Key, instance.Path.relString()))
				}
			}

			batch = append(batch, it)
		} else {
			fallback := e.resourceSource(entry.Path, entry.Resolved)
			r := e.resolveResourceInput(entry.Path, fallback)
			it := ResourceItem{Path: entry.Path, Key: entry.Key, DontParse: entry.DontParse}

			it.setInput(r.Input)

			if r.ProducerRef != 0 {
				cv := e.scanner.walkClosure(r.Input, e.moduleScanCtx(), scanDomainCC)
				auxBuilds := na.vfs.alloc(cv.len() + 1)[:0]

				for _, bucket := range cv.bucketList() {
					if bucket[0].isBuild() {
						auxBuilds = append(auxBuilds, bucket...)
					}
				}

				auxBuilds = append(auxBuilds, r.ProducerMainOut)
				na.vfs.commit(len(auxBuilds))
				it.AuxBuilds = auxBuilds[:len(auxBuilds):len(auxBuilds)]
			} else {
				it.AuxBuilds = na.vfsList(r.ProducerMainOut)
			}

			batch = append(batch, it)
		}

		if entry.EndsBatch {
			flushBatch()
		}
	}

	flushBatch()

	return refs, outs
}

func (e *EmitContext) emitPySourceGroup(gi int) {
	group := e.d.pySrcGroups[gi]

	e.collectPySrcGroup(gi, group)

	if e.producersOnly() {
		return
	}

	if isProgramModuleType(e.d.moduleStmt.Name) && e.d.unit.Tag == 0 && len(e.resources) == 0 && !e.d.pythonSourceCallback {
		return
	}

	if e.d.unit.Tag == unitTagPy3Bin {
		return
	}

	e.emitPySrcGroupObjcopy(gi, group)

	e.emitResourceFile(e.d.pyPyiResources[group.PyiStart:group.PyiEnd], e.d.unit.HashTag)

	if len(e.d.pySrcs) > 0 {
		e.emitPyProtoGroupResources(gi)
	}
}

func (e *EmitContext) resourceSource(token string, resolved ResolvedSource) VFS {
	if resolved.Path == 0 {
		resolved = resolveSource(e.ctx, e.instance, token, e.d.srcDirs)
	}

	return resolved.withGenerated(e.codegen, token, e.d.srcDirs)
}
