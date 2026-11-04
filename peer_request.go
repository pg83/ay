package main

import (
	"fmt"
	"slices"
	"strings"
)

const (
	peerRequested PeerRequest = 1 << (36 + iota)
	peerGo
	peerOptional
	peerExplicit
)

func genRequestedPeer(ctx *GenCtx, request ModuleInstance) *ModuleEmitResult {
	if entry := ctx.memoEntry(request); entry != nil {
		if entry.result == nil {
			return nil
		}

		if entry.resolved == 0 {
			if request.Demand != demandNone {
				ctx.consumeModuleWarnings(entry.result)
			} else if ctx.warningSink != nil && entry.result.deferredWarnings != nil {
				ctx.warningSink.dependencies = append(ctx.warningSink.dependencies, entry.result.deferredWarnings)
			}

			return entry.result
		}

		peer := request

		peer.Peer, peer.PeerFrom, peer.PeerTags = 0, 0, 0
		peer.Kind = ModuleKind(entry.resolved >> 8 & 31)
		peer.Language = Language(entry.resolved >> 1 & 15)
		peer.Demand = ModuleDemand(entry.resolved & variantDemandMask >> variantDemandShift)
		peer.YqlCurrent = entry.resolved&(1<<13) != 0

		if entry.resolved&variantPIC != 0 {
			peer.Platform = peer.Platform.pic()
		}

		return genModule(ctx, peer)
	}

	if request.Peer&peerOptional != 0 && !peerYaMakeExists(ctx.fs, request.Path) {
		ctx.memoPut(request, nil)

		return nil
	}

	var declaration *ModuleStmt

	exc := try(func() {
		declaration = ctx.evaluateDir(request.Path.relString(), ctx.target).declaration
	})

	if exc != nil {
		if request.Demand != demandNone && !ctx.keepGoing && ctx.emit.deferPublication == 0 {
			throw(exc)
		}

		warning := Warn{Kind: WarnModuleFailed, Message: fmt.Sprintf("%s: %s", request.Path.relString(), exc.Error())}
		result := &ModuleEmitResult{}

		if request.Demand == demandNone || ctx.emit.deferPublication > 0 {
			result.deferredWarnings = &moduleWarnings{own: []Warn{warning}}

			if ctx.warningSink != nil {
				ctx.warningSink.dependencies = append(ctx.warningSink.dependencies, result.deferredWarnings)
			}
		} else {
			ctx.onWarn(warning)
		}

		ctx.memoPut(request, result)

		return result
	}

	peer, result := ctx.resolvePeerInstance(request, declaration)

	if result == nil {
		if request.Peer&peerExplicit != 0 {
			ctx.onWarn(Warn{Kind: WarnModuleFailed, Message: fmt.Sprintf("PEERDIR from %s to %s is prohibited by incompatible tags and skipped", request.PeerFrom.relString(), request.Path.relString())})
		}

		ctx.memoPut(request, nil)

		return nil
	}

	entry := ctx.memoPut(request, result)

	entry.resolved = ctx.instanceVariant(peer)

	return result
}

type PeerRequest uint64

func makePeerRequest(d *ModuleData) PeerRequest {
	r := peerRequested

	if d.moduleStmt != nil && isGoModuleType(d.moduleStmt.Name) {
		r |= peerGo
	}

	return r
}

func consumerPeerTags(d *ModuleData, env Environment) STR {
	if d.confModule == nil || d.nots != nil {
		return 0
	}

	return internStr(strings.Join(strings.Fields(env.expandVariable(env.string(envPeerdirTags))), " "))
}

func (ctx *GenCtx) resolvePeerInstance(request ModuleInstance, declaration *ModuleStmt) (ModuleInstance, *ModuleEmitResult) {
	peer, rules := request, request.Peer

	peer.Peer, peer.PeerFrom, peer.PeerTags = 0, 0, 0

	var name TOK

	if declaration != nil {
		name = declaration.Name
	}

	if request.PeerTags != 0 {
		if result, selected := ctx.selectPeerVariant(&peer, request, declaration); selected {
			return peer, result
		}
	}

	if peer.Language == LangPy2 && !hasPy2Variant(name) {
		peer.Language = LangCPP
	}

	peer = ctx.finishPeerInstance(peer, rules, name)

	return peer, genModule(ctx, peer)
}

func (ctx *GenCtx) finishPeerInstance(peer ModuleInstance, rules PeerRequest, name TOK) ModuleInstance {
	if rules&peerGo != 0 && peer.Demand == demandNone && (peer.Language == LangGo || isGoModuleType(name)) {
		peer.Demand = demandSelf
	}

	if name == tokGoDll {
		peer.Platform = peer.Platform.pic()
	}

	if peer.Demand == demandNone && ctx.roots[peer] {
		peer.Demand = ctx.rootDemand
	}

	if rules&peerGo != 0 && name == tokRosLibrary && peer.Language == LangGo {
		cpp := peer

		cpp.Language, cpp.Demand = LangCPP, demandNone
		genModule(ctx, cpp)
	}

	return peer
}

func (ctx *GenCtx) selectPeerVariant(peer *ModuleInstance, request ModuleInstance, declaration *ModuleStmt) (*ModuleEmitResult, bool) {
	if declaration == nil {
		return nil, false
	}

	owner := multimoduleOwner(declaration)
	shapes := multimoduleShapes[owner]

	if shapes == nil {
		return nil, false
	}

	tags := ctx.peerTagSet(request.PeerTags)

	for _, variant := range peer.Platform.conf.peerVariants(owner) {
		if !slices.Contains(tags, variant.tag) {
			continue
		}

		shape, ok := shapes[variant.sub]

		if !ok {
			throwFmt("gen: %s: PEERDIR to %s selects %s of %s, which ay does not model", request.PeerFrom.relString(), peer.Path.relString(), variant.sub.string(), owner.string())
		}

		candidate := *peer

		candidate.Kind, candidate.Language, candidate.YqlCurrent = shape.kind, shape.language, shape.yqlCurrent
		candidate = ctx.finishPeerInstance(candidate, request.Peer, declaration.Name)

		result := genModule(ctx, candidate)

		if result.excluded {
			continue
		}

		*peer = candidate

		return result, true
	}

	return nil, true
}

func (ctx *GenCtx) peerTagSet(tags STR) []STR {
	if set, ok := ctx.peerTagSets[tags]; ok {
		return set
	}

	var set []STR

	for tag := range strings.FieldsSeq(tags.string()) {
		set = append(set, internStr(tag))
	}

	if ctx.peerTagSets == nil {
		ctx.peerTagSets = map[STR][]STR{}
	}

	ctx.peerTagSets[tags] = set

	return set
}
