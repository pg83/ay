package main

import (
	"bytes"
	"crypto/md5"
	encb64 "encoding/base64"
	enchex "encoding/hex"
	"slices"
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

	if src := e.resourceSources[resourceInputKey{rawPath, e.resourceDeclSeq}]; src != 0 {
		return ResolvedResource{Input: src}
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

func (e *EmitContext) buildObjcopyNode(oc *ObjcopyEmitCtx, kv *KV, outputObj VFS, payload []ANY, inputs InputChunks, deps []NodeRef) NodeRef {
	if info := e.codegen.lookup(outputObj); info != nil && info.ResourceObjcopy && info.OwnerModule == e.instance.Path && info.OwnerSerial != e.frame.serial {
		return info.ProducerRef
	}

	na := oc.na
	env := envVarsVCS

	node := Node{
		Platform:  e.instance.Platform,
		Cmds:      na.cmdList(Cmd{CmdArgs: objcopyCmdArgs(oc, outputObj, payload), Env: env}),
		Env:       env,
		Inputs:    inputs,
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
	Path        string
	Key         string
	Cmd         STR
	ExpandedCmd []ANY
	SourceInput VFS
	BuildInput  VFS
	AuxBuilds   []VFS
}

func (it ResourceItem) commandArgs() []ANY {
	if it.ExpandedCmd != nil {
		return it.ExpandedCmd
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

func resourceChunkEnds(items []ResourceItem, objcopy bool) []int {
	var ends []int

	cmdLen := 0

	for i, it := range items {
		if it.Path == "-" {
			cmdLen += rootCmdLen + len(it.Key)

			if !objcopy {
				cmdLen += len(it.Path)
			}
		} else if objcopy {
			cmdLen += rootCmdLen + len(it.Path) + encb64.StdEncoding.EncodedLen(len(it.Key))
		} else {
			cmdLen += rootCmdLen + len(it.Path) + len(it.Key)
		}

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

	for _, it := range p.Items {
		if resourceCanObjcopy(it.Path, it.Key) {
			objItems = append(objItems, it)
		} else {
			rawItems = append(rawItems, it)
		}
	}

	if len(objItems) > 0 {
		r, o := e.packObjcopyResourceChunks(objItems, p)

		refs = append(refs, r...)
		outs = append(outs, o...)
	}

	if len(rawItems) > 0 {
		var r []NodeRef
		var o []VFS

		if e.instance.Platform.ISA == ISAX8664 && !p.DontCompress && !p.RawCompile.ForceCxx {
			r, o = e.packYasmResourceChunks(rawItems, p)
		} else {
			if !p.RawCompile.ForceCxx {
				p.RawCompile = CompileSpec{ForceCxx: true, CFlags: e.ctx.na.anyList(argX.any(), argC.any())}
			}

			r, o = e.packRawResourceChunks(rawItems, p)
		}

		refs = append(refs, r...)
		outs = append(outs, o...)
	}

	return refs, outs
}

func (e *EmitContext) packObjcopyResourceChunks(items []ResourceItem, p ResourcePack) (refs []NodeRef, outs []VFS) {
	ctx, instance := e.ctx, e.instance
	na := ctx.na
	oc := e.objcopyEmitCtx()
	unitElem := "$S/" + instance.Path.relString()
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

		hashScratch = append(hashScratch, unitElem)

		var hash string

		hash, hashBuf = resourceHashInto(hashBuf, hashScratch, tag)

		outputObj := build(instance.Path.relString(), "/objcopy_", hash, ".o")
		deps := depRefs(oc.rescompilerLDRef, oc.rescompressorLDRef)

		refs = append(refs, e.buildObjcopyNode(oc, &pyObjcopyKV, outputObj, payload, inputs, deps))
		outs = append(outs, outputObj)
	}

	return refs, outs
}

func (e *EmitContext) packRawResourceChunks(items []ResourceItem, p ResourcePack) (refs []NodeRef, outs []VFS) {
	ctx, instance := e.ctx, e.instance
	na := ctx.na
	rescompilerRef, _ := ctx.tool(argToolsRescompiler)
	unitElem := "$S/" + instance.Path.relString()
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

		e.register(GeneratedFileInfo{
			OutputPath:    aux,
			ProducerRef:   auxRef,
			GeneratorRefs: na.refList(rescompilerRef),
		})

		nodeCmd := na.anys.alloc(commandWords)[:0]

		nodeCmd = append(nodeCmd, rescompilerBinVFS.any(), aux.any())

		for _, it := range chunk {
			if it.Path == "-" {
				nodeCmd = append(nodeCmd, dash.any())
				nodeCmd = append(nodeCmd, it.commandArgs()...)
			} else {
				nodeCmd = append(nodeCmd, it.input().any(), internV("-", it.Key).any())
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
			Platform:     instance.Platform,
			Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(nodeCmd), Env: env}),
			Env:          env,
			Inputs:       na.inputList(payloadBuilds, tail),
			InputClosure: Closure{self: aux},
			Outputs:      na.vfsList(aux),
			KV:           &rawAuxKV,
			DepRefs:      deps,
		}, auxRef)

		ccRef := ctx.emit.reserve()

		e.enqueueSrc(SrcMeta{
			Source: aux.any(), Prio: stmtPrioDefault,
			Compile: p.RawCompile, CompileRef: ccRef,
		})

		refs = append(refs, ccRef)
		outs = append(outs, e.ccOutputFor(aux, p.RawCompile))
	}

	return refs, outs
}

type ObjcopyEmitResult struct {
	DeclSeqs        []int
	Refs            []NodeRef
	Outputs         []VFS
	PySrcTrailCount int
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
	previousSeq := e.resourceDeclSeq

	defer func() { e.resourceDeclSeq = previousSeq }()

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
		e.resourceDeclSeq = entry.DeclSeq
		dontCompress = entry.DontCompress

		if entry.Path == "-" {
			it := ResourceItem{Path: "-"}

			if entry.SrcPath != "" {
				hashStart := len(e.resStrBuf)

				e.resStrBuf = append(e.resStrBuf, "resfs/src/"...)
				e.resStrBuf = append(e.resStrBuf, entry.Key...)
				e.resStrBuf = append(e.resStrBuf, "=${rootrel;context=TEXT;input=TEXT:\""...)
				e.resStrBuf = append(e.resStrBuf, entry.SrcPath...)
				e.resStrBuf = append(e.resStrBuf, "\"}"...)

				it.Key = bytesString(e.resStrBuf[hashStart:])

				r := e.resolveResourceInput(entry.SrcPath, copyFileInputVFS(ctx.fs, instance.Path, entry.SrcPath))
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
					r := e.resolveResourceInput(inner, copyFileInputVFS(ctx.fs, instance.Path, inner))

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
			it := ResourceItem{Path: entry.Path, Key: entry.Key}

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

func (e *EmitContext) emitResourceObjcopy() *ObjcopyEmitResult {
	_, _, d := e.ctx, e.instance, e.d
	batchOrder := e.resourceActionOrder()
	hasKvOnly := d.pyMain != nil || len(d.noCheckImports) > 0 || e.hasEnginePySrcs() || len(d.yaConfJSON) > 0

	if len(e.resources) == 0 && len(d.pyPyiResources) == 0 && !hasKvOnly {
		return nil
	}

	out := &ObjcopyEmitResult{}
	pyMainRefs, pyMainOuts := e.emitPyMainObjcopy()

	out.Refs = append(out.Refs, pyMainRefs...)
	out.Outputs = append(out.Outputs, pyMainOuts...)

	noCheckRefs, noCheckOuts := e.emitNoCheckImportsObjcopy()

	out.Refs = append(out.Refs, noCheckRefs...)
	out.Outputs = append(out.Outputs, noCheckOuts...)

	yaConfRefs, yaConfOuts := e.emitYaConfJSONObjcopy()

	out.Refs = append(out.Refs, yaConfRefs...)
	out.Outputs = append(out.Outputs, yaConfOuts...)

	moduleTag := d.unit.HashTag
	py3BinProgramSide := d.unit.Tag == unitTagPy3Bin
	resourceOffset := 0
	trailStart := -1

	var pending []resourceQueueBatch

	emitResources := func(entries []ResourceEntry) {
		for len(entries) > 0 {
			n := 1

			for n < len(entries) && !entries[n-1].EndsBatch {
				n++
			}

			batch := entries[:n]

			entries = entries[n:]

			if order, ok := batchOrder[batch[0].DeclSeq]; ok {
				pending = append(pending, resourceQueueBatch{entries: batch, order: order})

				continue
			}

			r, o := e.emitResourceFile(batch, moduleTag)

			out.Refs = append(out.Refs, r...)
			out.Outputs = append(out.Outputs, o...)
		}
	}

	emitGroup := func(gi int, group PySrcGroup) {
		before := len(out.Refs)

		e.emitPySrcGroupObjcopy(gi, group, out)

		if !py3BinProgramSide {
			r, o := e.emitResourceFile(d.pyPyiResources[group.PyiStart:group.PyiEnd], moduleTag)

			out.Refs = append(out.Refs, r...)
			out.Outputs = append(out.Outputs, o...)
		}

		for len(out.DeclSeqs) < len(out.Refs) {
			out.DeclSeqs = append(out.DeclSeqs, 0)
		}

		for i := before; i < len(out.Refs); i++ {
			out.DeclSeqs[i] = group.DeclSeq
		}

		if trailStart < 0 && len(out.Refs) > before {
			trailStart = before
		}
	}

	for gi, group := range d.pySrcGroups {
		if group.Prio != stmtPrioDefault {
			continue
		}

		if !py3BinProgramSide {
			emitResources(e.resources[resourceOffset:group.ResourceOffset])

			resourceOffset = group.ResourceOffset
		}

		emitGroup(gi, group)
	}

	if !py3BinProgramSide {
		emitResources(e.resources[resourceOffset:])
	}

	for gi, group := range d.pySrcGroups {
		if group.Prio != stmtPrioDefault {
			emitGroup(gi, group)
		}
	}

	slices.SortFunc(pending, func(a, b resourceQueueBatch) int { return a.order - b.order })

	for _, batch := range pending {
		r, o := e.emitResourceFile(batch.entries, moduleTag)

		out.Refs = append(out.Refs, r...)
		out.Outputs = append(out.Outputs, o...)
	}

	if trailStart < 0 {
		trailStart = len(out.Refs)
	}

	out.PySrcTrailCount = len(out.Refs) - trailStart

	return out
}
