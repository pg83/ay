package main

// post_emit.go — per-node mutator hook returned by newPostEmitPrepare.
//
// Currently a no-op: the back-peer and umbrella ADDINCL post-emit
// patches both lived here and were removed (synthetic injections that
// upstream ymake does not perform; both turned out to be dead on the
// M2 + M3 reference graph). The plumbing (`mightNeedAddInclPatch`
// hold predicate + `newPostEmitPrepare` factory) is retained as a
// hook point should a legitimate post-emit transformation arise.

// mightNeedAddInclPatch is the hold predicate used by StreamingEmitter
// (yatool make). Currently no CC nodes need deferral — returns false
// for everything so streaming emitter finalises every node inline.
func mightNeedAddInclPatch(n *Node) bool {
	return false
}

func newPostEmitPrepare(ctx *genCtx) func(*Node) {
	return func(n *Node) {}
}
