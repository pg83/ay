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
	resKvMacroPrefix            = []byte("${ARCADIA")
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
	SourceInputs    []VFS
}

func (e *EmitContext) resolveResourceInput(rawPath string, fallback VFS) ResolvedResource {
	_, instance := e.ctx, e.instance
	output := resourceOutputVFS(instance.Path.relString(), rawPath)

	if info := e.codegen.use(output); info != nil {
		return ResolvedResource{
			Input:           output,
			ProducerRef:     info.ProducerRef,
			ProducerMainOut: info.ProducerMainOut,
			SourceInputs:    info.SourceInputs,
		}
	}

	return ResolvedResource{Input: fallback}
}

func (e *EmitContext) buildObjcopyNode(oc *ObjcopyEmitCtx, kv *KV, outputObj VFS, payload []ANY, inputs InputChunks, deps []NodeRef) NodeRef {
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

	return e.emitNode(node)
}

type ResourceItem struct {
	Path         string
	Key          string
	Cmd          STR
	SourceInput  VFS
	BuildInput   VFS
	ExtraSources []VFS
	ExtraBuilds  []VFS
	AuxSources   []VFS
	AuxBuilds    []VFS
}

func (it ResourceItem) input() VFS {
	if it.BuildInput != 0 {
		return it.BuildInput
	}

	return it.SourceInput
}

type ResourcePack struct {
	Tag        STR
	Items      []ResourceItem
	RawCompile CompileSpec
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

		nPaths, nKvs, sourceBound, buildBound := 0, 0, 0, 0

		for _, it := range chunk {
			if it.Path == "-" {
				nKvs++
			} else {
				nPaths++
			}

			if it.SourceInput != 0 {
				sourceBound++
			}

			if it.BuildInput != 0 {
				buildBound++
			}

			sourceBound += len(it.ExtraSources) + len(it.AuxSources)
			buildBound += len(it.ExtraBuilds) + len(it.AuxBuilds)
		}

		payloadCap := 0

		if nPaths > 0 {
			payloadCap += 2 + 2*nPaths
		}

		if nKvs > 0 {
			payloadCap += 1 + nKvs
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
					payload = append(payload, it.Cmd.any())
					hashScratch = append(hashScratch, it.Key)
				}
			}
		}

		na.anys.commit(len(payload))

		payload = payload[:len(payload):len(payload)]

		var adjacentSources, adjacentBuilds, tailSources, tailBuilds []VFS

		dedupers.with(func(deduper *DeDuper) {
			adjacentSources = na.vfs.alloc(sourceBound)[:0]

			for _, it := range chunk {
				if it.SourceInput != 0 && deduper.add(it.SourceInput.strID()) {
					adjacentSources = append(adjacentSources, it.SourceInput)
				}

				for _, v := range it.ExtraSources {
					if v != 0 && deduper.add(v.strID()) {
						adjacentSources = append(adjacentSources, v)
					}
				}
			}

			na.vfs.commit(len(adjacentSources))
			adjacentSources = adjacentSources[:len(adjacentSources):len(adjacentSources)]
			adjacentBuilds = na.vfs.alloc(buildBound)[:0]

			for _, it := range chunk {
				if it.BuildInput != 0 && deduper.add(it.BuildInput.strID()) {
					adjacentBuilds = append(adjacentBuilds, it.BuildInput)
				}

				for _, v := range it.ExtraBuilds {
					if v != 0 && deduper.add(v.strID()) {
						adjacentBuilds = append(adjacentBuilds, v)
					}
				}
			}

			na.vfs.commit(len(adjacentBuilds))
			adjacentBuilds = adjacentBuilds[:len(adjacentBuilds):len(adjacentBuilds)]

			for _, v := range rescompilersWithScriptChunk {
				deduper.add(v.strID())
			}

			tailSources = na.vfs.alloc(sourceBound)[:0]

			for _, it := range chunk {
				for _, v := range it.AuxSources {
					if v != 0 && deduper.add(v.strID()) {
						tailSources = append(tailSources, v)
					}
				}
			}

			na.vfs.commit(len(tailSources))
			tailSources = tailSources[:len(tailSources):len(tailSources)]
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

		inputs := na.inputList(rescompilersChunk, adjacentSources, adjacentBuilds, objcopyScriptChunk, tailSources, tailBuilds)

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

	if !p.RawCompile.ForceCxx {
		throwFmt("packResources: %s has raw-routed resource items but no RawCompile", instance.Path.relString())
	}

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

		sourceBound, buildBound := 0, 0

		for _, it := range chunk {
			if it.SourceInput != 0 {
				sourceBound++
			}

			if it.BuildInput != 0 {
				buildBound++
			}

			sourceBound += len(it.ExtraSources)
			buildBound += len(it.ExtraBuilds)
		}

		var adjacentSources, payloadSources, payloadBuilds []VFS

		hashScratch = hashScratch[:0]

		dedupers.with(func(deduper *DeDuper) {
			adjacentSources = na.vfs.alloc(sourceBound)[:0]

			for _, it := range chunk {
				if it.Path == "-" {
					hashScratch = append(hashScratch, "-", it.Key)
				} else {
					dashStart := len(dashBuf)

					dashBuf = append(dashBuf, '-')
					dashBuf = append(dashBuf, it.Key...)

					hashScratch = append(hashScratch, it.Path, bytesString(dashBuf[dashStart:]))
				}

				if it.SourceInput != 0 && deduper.add(it.SourceInput.strID()) {
					adjacentSources = append(adjacentSources, it.SourceInput)
				}

				for _, v := range it.ExtraSources {
					if v != 0 && deduper.add(v.strID()) {
						adjacentSources = append(adjacentSources, v)
					}
				}
			}

			na.vfs.commit(len(adjacentSources))
			adjacentSources = adjacentSources[:len(adjacentSources):len(adjacentSources)]
		})

		dedupers.with(func(deduper *DeDuper) {
			payloadSources = na.vfs.alloc(sourceBound)[:0]

			for _, it := range chunk {
				if it.Path != "-" && it.SourceInput != 0 && deduper.add(it.SourceInput.strID()) {
					payloadSources = append(payloadSources, it.SourceInput)
				}
			}

			na.vfs.commit(len(payloadSources))
			payloadSources = payloadSources[:len(payloadSources):len(payloadSources)]
			payloadBuilds = na.vfs.alloc(buildBound)[:0]

			for _, it := range chunk {
				if it.Path != "-" && it.BuildInput != 0 && deduper.add(it.BuildInput.strID()) {
					payloadBuilds = append(payloadBuilds, it.BuildInput)
				}
			}

			na.vfs.commit(len(payloadBuilds))
			payloadBuilds = payloadBuilds[:len(payloadBuilds):len(payloadBuilds)]
		})
		hashScratch = append(hashScratch, unitElem)

		var hash string

		hash, hashBuf = resourceHashInto(hashBuf, hashScratch, tag)

		aux := build(instance.Path.relString(), "/", hash, "_raw.auxcpp")
		auxRef := ctx.emit.reserve()
		seed := na.dedupSourceVFS(adjacentSources, nil)
		emits := na.dirs.alloc(len(seed))[:0]

		for _, v := range seed {
			emits = append(emits, IncludeDirective{kind: includeQuoted, target: includeTarget(v.rel().any())})
		}

		na.dirs.commit(len(emits))
		emits = emits[:len(emits):len(emits)]

		e.register(GeneratedFileInfo{
			OutputPath:     aux,
			ProducerRef:    auxRef,
			GeneratorRefs:  na.refList(rescompilerRef),
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: emits},
		})

		nodeCmd := na.anys.alloc(2 + 2*len(chunk))[:0]

		nodeCmd = append(nodeCmd, rescompilerBinVFS.any(), aux.any())

		for _, it := range chunk {
			if it.Path == "-" {
				nodeCmd = append(nodeCmd, dash.any(), it.Cmd.any())
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
			Platform: instance.Platform,
			Cmds:     na.cmdList(Cmd{CmdArgs: na.chunkList(nodeCmd), Env: env}),
			Env:      env,
			Inputs:   na.inputList(payloadSources, payloadBuilds, tail),
			Outputs:  na.vfsList(aux),
			KV:       &rawAuxKV,
			DepRefs:  deps,
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
	na := ctx.na
	batch := e.resItems[:0]

	defer func() { e.resItems = retainMaxLen(e.resItems, batch) }()

	flushBatch := func() {
		if len(batch) == 0 {
			return
		}

		r, o := e.packResources(ResourcePack{Tag: moduleTag, Items: batch})

		refs = append(refs, r...)
		outs = append(outs, o...)
		e.resItems = retainMaxLen(e.resItems, batch)
		batch = batch[:0]
	}

	for _, entry := range entries {
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
					it.Cmd = internStr(renderResourceKvCmd(string(cmdView)))
				} else {
					it.Cmd = internBytes(cmdView)
				}

				e.resStrBuf = e.resStrBuf[:cmdStart]
				it.setInput(r.Input)
				it.AuxBuilds = na.vfsList(r.ProducerMainOut)
			} else {
				it.Key = entry.Key

				if inner, ok := rootrelInputPath(entry.Key); ok {
					r := e.resolveResourceInput(inner, copyFileInputVFS(ctx.fs, instance.Path, inner))

					it.Cmd = internStr(renderResourceKvCmd(rootrelExpand(entry.Key, r.Input.relString())))
					it.setInput(r.Input)
					it.AuxBuilds = na.vfsList(r.ProducerMainOut)
				} else {
					it.Cmd = internStr(renderResourceKvCmd(entry.Key))
				}
			}

			batch = append(batch, it)
		} else {
			r := e.resolveResourceInput(entry.Path, copyFileInputVFS(ctx.fs, instance.Path, entry.Path))
			it := ResourceItem{Path: entry.Path, Key: entry.Key}

			it.setInput(r.Input)

			if r.ProducerRef != 0 {
				cv := e.scanner.walkClosure(r.Input, d.scanCtx, scanDomainCC)
				auxSources := na.vfs.alloc(len(r.SourceInputs))[:0]

				for _, v := range r.SourceInputs {
					if objcopySourceLeafKept(v.relString()) {
						auxSources = append(auxSources, v)
					}
				}

				na.vfs.commit(len(auxSources))
				it.AuxSources = auxSources[:len(auxSources):len(auxSources)]
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

	if len(e.resources) == 0 && len(d.pyPyiResources) == 0 {
		trailStart := len(out.Refs)
		srcRes := e.emitPySrcObjcopy()

		if srcRes != nil {
			out.Refs = append(out.Refs, srcRes.Refs...)
			out.Outputs = append(out.Outputs, srcRes.Outputs...)
		}

		out.PySrcTrailCount = len(out.Refs) - trailStart

		return out
	}

	moduleTag := d.unit.Tag
	py3BinProgramSide := d.unit.Tag == unitTagPy3Bin

	if !py3BinProgramSide {
		r, o := e.emitResourceFile(e.resources, moduleTag)

		out.Refs = append(out.Refs, r...)
		out.Outputs = append(out.Outputs, o...)
	}

	trailStart := len(out.Refs)
	srcRes := e.emitPySrcObjcopy()

	if srcRes != nil {
		out.Refs = append(out.Refs, srcRes.Refs...)
		out.Outputs = append(out.Outputs, srcRes.Outputs...)
	}

	if !py3BinProgramSide {
		r, o := e.emitResourceFile(d.pyPyiResources, moduleTag)

		out.Refs = append(out.Refs, r...)
		out.Outputs = append(out.Outputs, o...)
	}

	out.PySrcTrailCount = len(out.Refs) - trailStart

	return out
}
