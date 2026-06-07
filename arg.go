package main

var argTable = struct {
	ids  map[string]ARG
	strs []string
}{
	ids:  make(map[string]ARG, 256),
	strs: make([]string, 1, 256), // index 0 reserved: zero-value ARG is the empty arg
}

// argDedupeSeen dedups []ARG preserving first-occurrence order, via a reused
// epoch idSet sized to the arg space (ARG is uint32, cast to VFS only as the
// set's key). Single-threaded gen, leaf use (reset → scan → return); kept
// separate from the VFS deduper so the two uint32 id-spaces never interleave.
var argDedupeSeen idSet

// ARG is a dense interned id for a single compiler/linker argument token (e.g.
// "-mavx2", "-DFOO=1"). Args are few and string identity is exact, so the table
// is a plain map + vector — no hashing games. One global namespace; args never
// share the VFS/STR path-intern space. The string form is recovered only when a
// node's CmdArgs are built (emit/compose), analogous to VFS→string at JSON write.
type ARG uint32

func internArg(s string) ARG {
	if id, ok := argTable.ids[s]; ok {
		return id
	}

	id := ARG(len(argTable.strs))
	argTable.strs = append(argTable.strs, s)
	argTable.ids[s] = id

	return id
}

func (a ARG) String() string {
	return argTable.strs[a]
}

// internArgs interns each input as one whole ARG (no whitespace split — a value
// like `-D__DATE__="Jan 10 2019"` is a single argument). Multi-token env values
// (e.g. "-mavx2 -mfma …") are already split into individual tokens upstream at
// the env-expansion boundary (strings.Fields), so every element here is already
// one cmd_args token.
func internArgs(ss []string) []ARG {
	if len(ss) == 0 {
		return nil
	}

	out := make([]ARG, len(ss))

	for i, s := range ss {
		out[i] = internArg(s)
	}

	return out
}

// appendArgStrs materializes the given arg slices straight onto a cmd_args
// []string with no intermediate allocation — the compose-side boundary.
func appendArgStrs(dst []string, srcs ...[]ARG) []string {
	for _, s := range srcs {
		for _, a := range s {
			dst = append(dst, argTable.strs[a])
		}
	}

	return dst
}

// argStrs materializes args back to strings — only at the cmd_args boundary.
func argStrs(as []ARG) []string {
	if len(as) == 0 {
		return nil
	}

	out := make([]string, len(as))

	for i, a := range as {
		out[i] = argTable.strs[a]
	}

	return out
}

func argBound() uint32 {
	return uint32(len(argTable.strs))
}

// addEachARG appends each arg of src not already in seen to *dst, recording it
// in seen — the order-preserving union used by the peer-flag aggregation. seen
// is a BitSet over the ARG space (the "map флагов → битсет" replacement).
func addEachARG(seen *BitSet, dst *[]ARG, src []ARG) {
	for _, x := range src {
		if seen.has(uint32(x)) {
			continue
		}

		seen.add(uint32(x))
		*dst = append(*dst, x)
	}
}

func dedupARG(lists ...[]ARG) []ARG {
	argDedupeSeen.reset(argBound())

	var out []ARG

	for _, l := range lists {
		for _, a := range l {
			if !argDedupeSeen.has(VFS(a)) {
				argDedupeSeen.add(VFS(a))
				out = append(out, a)
			}
		}
	}

	return out
}
