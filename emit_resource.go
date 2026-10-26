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
	oc.rescompilerLDRef, _ = ctx.tool(argToolsRescompiler)
	oc.rescompressorLDRef, _ = ctx.tool(argToolsRescompressor)
	oc.blocks = composeObjcopyArgBlocks(ctx.na, d.tc, instancePlatform(e))

	return oc
}

func instancePlatform(e *EmitContext) *Platform {
	return e.instance.Platform
}

func composeObjcopyArgBlocks(na *NodeArenas, tc ModuleToolchain, p *Platform) ObjcopyArgBlocks {
	return ObjcopyArgBlocks{
		pre: na.anyList(
			tc.Python3.any(),
			objcopyScriptVFS.any(),
			argCompiler.any(), tc.CXX.any(),
			argObjcopy.any(), tc.Objcopy.any(),
			argCompressor.any(), rescompressorBinVFS.any(),
			argRescompiler.any(), rescompilerBinVFS.any(),
			argOutputObj.any(),
		),
		post: na.anyList(argTarget.any(), internStr(p.Triple).any()),
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

	node := Node{
		Platform: e.instance.Platform,
		Cmds:     na.cmdList(Cmd{CmdArgs: objcopyCmdArgs(oc, outputObj, payload), Resources: resources, Env: env}),
		Env:      env,
		Inputs:   NodeInputs{files: inputs, roots: buildFrom, text: textInputs},

		Outputs:   na.vfsList(outputObj),
		KV:        kv,
		Resources: e.instance.Platform.UsesPython3Clang,
		DepRefs:   deps,
	}

	ref := e.emitNode(node)

	e.register(GeneratedFileInfo{OutputPath: outputObj, ProducerRef: ref, ResourceObjcopy: true})

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

func (it ResourceItem) commandArgs() []ANY {
	if it.ExpandedCmd.Args != nil {
		return it.ExpandedCmd.Args
	}

	return []ANY{it.Cmd.any()}
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
	var inputs []VFS

	dedupers.with(func(dd *DeDuper) {
		for _, item := range items {
			if !item.DontParse {
				dd.addStable(item.input().strID())
			}
		}

		for _, item := range items {
			input := item.input()

			if item.DontParse && input != 0 && input.isSource() && dd.addStable(input.strID()) {
				inputs = append(inputs, input)
			}
		}
	})

	return e.ctx.na.vfsList(inputs...)
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

func resourceChunkEnds(items []ResourceItem, objcopy bool) []int {
	var ends []int

	cmdLen := 0

	for i, it := range items {
		cmdLen += resourceItemCmdLen(it, objcopy)

		if cmdLen >= maxCmdLen {
			ends = append(ends, i+1)
			cmdLen = 0
		}
	}

	if cmdLen > 0 {
		ends = append(ends, len(items))
	}

	return ends
}

func (e *EmitContext) packResources(p ResourcePack) (refs []NodeRef, outs []VFS) {
	objItems := e.objScratch[:0]
	rawItems := e.rawScratch[:0]

	defer func() {
		e.objScratch = retainMaxLen(e.objScratch, objItems)
		e.rawScratch = retainMaxLen(e.rawScratch, rawItems)
	}()

	flushObj := func() {
		if len(objItems) == 0 {
			return
		}

		r, o := e.packObjcopyResourceChunks(objItems, p)

		refs = append(refs, r...)
		outs = append(outs, o...)
		e.objScratch = retainMaxLen(e.objScratch, objItems)
		objItems = objItems[:0]
	}

	useYasm := e.instance.Platform.ISA == ISAX8664 && !p.DontCompress && !p.RawCompile.ForceCxx

	flushRaw := func() {
		if len(rawItems) == 0 {
			return
		}

		var r []NodeRef
		var o []VFS

		if useYasm {
			r, o = e.packYasmResourceChunks(rawItems, p)
		} else {
			if !p.RawCompile.ForceCxx {
				p.RawCompile = CompileSpec{ForceCxx: true, CFlags: e.ctx.na.anyList(argX.any(), argC.any())}
			}

			r, o = e.packRawResourceChunks(rawItems, p)
		}

		refs = append(refs, r...)
		outs = append(outs, o...)
		e.rawScratch = retainMaxLen(e.rawScratch, rawItems)
		rawItems = rawItems[:0]
	}

	objLen, rawLen := 0, 0

	for _, it := range p.Items {
		if resourceCanObjcopy(it.Path, it.Key) {
			objItems = append(objItems, it)
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

	return refs, outs
}

func (e *EmitContext) packObjcopyResourceChunks(items []ResourceItem, p ResourcePack) (refs []NodeRef, outs []VFS) {
	ctx, instance := e.ctx, e.instance
	na := ctx.na
	oc := e.objcopyEmitCtx()
	unitElem := joinRel("$S", cleanRel(instance.Path.relString()))
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

	lo := 0

	for _, hi := range resourceChunkEnds(items, true) {
		chunk := items[lo:hi]

		lo = hi

		nPaths, nKvs, nKvArgs, sourceBound, buildBound := 0, 0, 0, 0, 0

		for _, it := range chunk {
			if it.Path == "-" {
				nKvs++
				nKvArgs += len(it.commandArgs())
			} else {
				nPaths++
			}

			if it.SourceInput != 0 {
				sourceBound++
			}

			if it.BuildInput != 0 {
				buildBound++
			}

			buildBound += len(it.AuxBuilds)
		}

		payloadCap := 0

		if nPaths > 0 {
			payloadCap += 2 + 2*nPaths
		}

		if nKvs > 0 {
			payloadCap += 1 + nKvArgs
		}

		payload := na.anys.alloc(payloadCap)[:0]

		var resourceUses []STR

		hashScratch = hashScratch[:0]

		if nPaths > 0 {
			payload = append(payload, argInputs.any())

			for _, it := range chunk {
				if it.Path != "-" {
					payload = append(payload, it.input().any())
					hashScratch = append(hashScratch, it.Path)
				}
			}

			payload = append(payload, argKeys.any())

			for _, it := range chunk {
				if it.Path != "-" {
					b64Scratch = encb64.StdEncoding.AppendEncode(b64Scratch[:0], strBytes(it.Key))

					key := internBytes(b64Scratch)

					payload = append(payload, key.any())
					hashScratch = append(hashScratch, key.string())
				}
			}
		}

		if nKvs > 0 {
			payload = append(payload, argKvs.any())

			for _, it := range chunk {
				if it.Path == "-" {
					payload = append(payload, it.commandArgs()...)
					resourceUses = resourceKeyUnion(resourceUses, it.ExpandedCmd.Resources)
					hashScratch = append(hashScratch, it.Key)
				}
			}
		}

		na.anys.commit(len(payload))

		payload = payload[:len(payload):len(payload)]

		var adjacentSources, adjacentBuilds, tailBuilds []VFS

		dedupers.with(func(deduper *DeDuper) {
			adjacentSources = na.vfs.alloc(sourceBound)[:0]

			for _, it := range chunk {
				if it.SourceInput != 0 && deduper.add(it.SourceInput.strID()) {
					adjacentSources = append(adjacentSources, it.SourceInput)
				}
			}

			na.vfs.commit(len(adjacentSources))
			adjacentSources = adjacentSources[:len(adjacentSources):len(adjacentSources)]
			adjacentBuilds = na.vfs.alloc(buildBound)[:0]

			for _, it := range chunk {
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

			for _, it := range chunk {
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

		outputObj := buildJoined(instance.Path.relString(), "objcopy_"+hash+".o")
		deps := depRefs(oc.rescompilerLDRef, oc.rescompressorLDRef)

		refs = append(refs, e.buildObjcopyNode(oc, &pyObjcopyKV, outputObj, payload, resourceUses, inputs, buildFrom, e.resourceTextInputs(chunk), deps))

		meta := e.resourceSourceMeta(chunk, outputObj)

		e.collectObj(refs[len(refs)-1], outputObj, meta)

		outs = append(outs, outputObj)
	}

	return refs, outs
}

func (e *EmitContext) packRawResourceChunks(items []ResourceItem, p ResourcePack) (refs []NodeRef, outs []VFS) {
	ctx, instance := e.ctx, e.instance
	na := ctx.na
	rescompilerRef, _ := ctx.tool(argToolsRescompiler)
	unitElem := joinRel("$S", cleanRel(instance.Path.relString()))
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

	lo := 0

	for _, hi := range resourceChunkEnds(items, false) {
		chunk := items[lo:hi]

		lo = hi

		buildBound, commandWords := 0, 2

		for _, it := range chunk {
			commandWords += 1 + len(it.commandArgs())

			if it.input() != 0 {
				buildBound++
			}
		}

		var payloadBuilds []VFS

		hashScratch = hashScratch[:0]

		for _, it := range chunk {
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

			for _, it := range chunk {
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

		e.register(GeneratedFileInfo{
			OutputPath:    aux,
			ProducerRef:   auxRef,
			GeneratorRefs: na.refList(rescompilerRef),
		})

		nodeCmd := na.anys.alloc(commandWords)[:0]

		nodeCmd = append(nodeCmd, rescompilerBinVFS.any(), aux.any())
		var resourceUses []STR

		for _, it := range chunk {
			if it.Path == "-" {
				nodeCmd = append(nodeCmd, dash.any())
				nodeCmd = append(nodeCmd, it.commandArgs()...)

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

		e.emitReservedNode(Node{
			Platform: instance.Platform,
			Cmds:     na.cmdList(Cmd{CmdArgs: na.chunkList(nodeCmd), Resources: resourceUses, Env: env}),
			Env:      env,
			Inputs:   NodeInputs{files: na.inputList(payloadBuilds, tail), roots: na.inputList(payloadBuilds), text: e.resourceTextInputs(chunk)},

			Outputs: na.vfsList(aux),
			KV:      &rawAuxKV,
			DepRefs: deps,
		}, auxRef)

		ccRef := ctx.emit.reserve()

		meta.Compile, meta.CompileRef = p.RawCompile, ccRef

		e.emitOneSource(meta)
		refs = append(refs, ccRef)
		outs = append(outs, e.ccOutputFor(aux, p.RawCompile))
	}

	return refs, outs
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
	ctx, instance, d := e.ctx, e.instance, e.d
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

				r := e.resolveResourceInput(entry.SrcPath, e.resolveModuleSourceVFS(internStr(entry.SrcPath).any(), d.srcDirs))
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
					r := e.resolveResourceInput(inner, e.resolveModuleSourceVFS(internStr(inner).any(), d.srcDirs))

					it.Cmd = internStr(renderResourceKvCmd(head+r.Input.relString()+tail, instance.Path.relString()))
					it.setInput(r.Input)
					it.AuxBuilds = na.vfsList(r.ProducerMainOut)
				} else {
					it.Cmd = internStr(renderResourceKvCmd(entry.Key, instance.Path.relString()))
				}
			}

			batch = append(batch, it)
		} else {
			fallback := e.resolveModuleSourceVFS(internStr(entry.Path).any(), d.srcDirs)
			r := e.resolveResourceInput(entry.Path, fallback)
			it := ResourceItem{Path: entry.Path, Key: entry.Key, DontParse: entry.DontParse}

			it.setInput(r.Input)

			if r.ProducerRef != 0 {
				cv := e.scanner.walkClosure(r.Input, d.scanCtx, scanDomainCC)
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

	e.emitPySrcGroupObjcopy(gi, group, &ObjcopyEmitResult{})

	if e.d.unit.Tag != unitTagPy3Bin {
		e.emitResourceFile(e.d.pyPyiResources[group.PyiStart:group.PyiEnd], e.d.unit.HashTag)
	}
}
