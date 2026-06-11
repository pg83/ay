package main

// argTable stores no strings of its own: the string lives once in the global STR
// intern table, and an ARG is just a second dense id layered on top. ids maps a
// token's STR to its ARG; strs maps an ARG back to that STR — a free, O(1)
// ARG→STR conversion (ARG.str()), the basis for dropping ANY in favour of plain
// STR in CmdArgs. VFS/TOK already share the STR backing the same way.
var argTable = struct {
	ids  DenseMap[STR, uint32]
	strs []STR
}{
	strs: make([]STR, 1, 256), // index 0 reserved: zero-value ARG is the empty arg
}

// ARG is a dense interned id for a single compiler/linker argument token (e.g.
// "-mavx2", "-DFOO=1"). One global namespace, layered on the STR table: the
// token's string is interned once as a STR and the ARG records that STR, so
// ARG→STR (str()) and ARG→string (String()) are O(1) array loads.
type ARG uint32

func internArg(s string) ARG {
	return internArgSTR(internStr(s))
}

// internArgSTR interns an already-interned token STR into the ARG namespace.
func internArgSTR(st STR) ARG {
	if id, ok := argTable.ids.get(st); ok {
		return ARG(id)
	}

	id := ARG(len(argTable.strs))
	argTable.strs = append(argTable.strs, st)
	argTable.ids.put(st, uint32(id))

	return id
}

// str returns the STR backing this ARG — a free conversion (no re-interning).
func (a ARG) str() STR {
	return argTable.strs[a]
}

func (a ARG) String() string {
	return argTable.strs[a].String()
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
			dst = append(dst, a.String())
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
		out[i] = a.String()
	}

	return out
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

// dedupARG unions []ARG lists preserving first-occurrence order, routing through
// the program-global VFS deduper with each ARG id cast to VFS as the set key. The
// deduper is reset first, so only this call's ARG-derived keys live in the set —
// the ARG and VFS id-spaces never interleave within a single dedup pass.
func dedupARG(lists ...[]ARG) []ARG {
	deduper.reset()

	var out []ARG

	for _, l := range lists {
		for _, a := range l {
			if deduper.add(VFS(a)) {
				out = append(out, a)
			}
		}
	}

	return out
}
