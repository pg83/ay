package main

import (
	"slices"
	"strings"
)

var runtimeAncestorCxxConsumers = map[string]bool{
	"library/cpp/malloc/api": true,
}

func (e *EmitContext) confPeerdirs() []string {
	peers := e.peerScratch[:0]

	defer func() { e.peerScratch = retainMaxLen(e.peerScratch, peers) }()

	self := e.instance.Path.relString()

	for peer := range strings.FieldsSeq(e.d.basePeerdirs.string()) {
		if peer != self && !slices.Contains(peers, peer) {
			peers = append(peers, peer)
		}
	}

	return peers
}

func effectiveNoPlatform(f FlagSet) bool {
	if f.NoPlatform {
		return true
	}

	return f.NoLibc && f.NoRuntime && f.NoUtil
}

func peerYaMakeExists(fs FS, peerDir VFS) bool {
	return fs.isFile(peerDir.rel(), "ya.make")
}
