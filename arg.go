package main

// argTable stores no strings of its own: an ARG is a second dense id layered on
// the global STR table. ids maps STR→ARG, strs maps ARG→STR for free O(1)
// conversion.
var argTable = struct {
	ids  DenseMap[STR, uint32]
	strs []STR
}{
	strs: make([]STR, 1, 256), // index 0: zero-value ARG is the empty arg
}

// ARG is a dense interned id for a single compiler/linker argument token,
// layered on the STR table so str() and String() are O(1) array loads.
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

// str returns the STR backing this ARG, no re-interning.
func (a ARG) str() STR {
	return argTable.strs[a]
}

func (a ARG) string() string {
	return argTable.strs[a].string()
}

// String implements fmt.Stringer; internal code calls string().
func (a ARG) String() string {
	return a.string()
}

// internArgs interns each input as one whole ARG, no whitespace split: each
// element is already one cmd_args token, split upstream.
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

// appendArgStrs materializes arg slices onto a cmd_args []string with no
// intermediate allocation.
func appendArgStrs(dst []string, srcs ...[]ARG) []string {
	for _, s := range srcs {
		for _, a := range s {
			dst = append(dst, a.string())
		}
	}

	return dst
}

// argStrs materializes args back to strings, only at the cmd_args boundary.
func argStrs(as []ARG) []string {
	if len(as) == 0 {
		return nil
	}

	out := make([]string, len(as))

	for i, a := range as {
		out[i] = a.string()
	}

	return out
}

// addEachARG appends each arg of src not in seen to *dst, recording it: the
// order-preserving union for peer-flag aggregation.
func addEachARG(seen *BitSet, dst *[]ARG, src []ARG) {
	for _, x := range src {
		if seen.has(uint32(x)) {
			continue
		}

		seen.add(uint32(x))
		*dst = append(*dst, x)
	}
}

// dedupARG unions []ARG lists preserving first-occurrence order via the global
// VFS deduper, reset first so ARG and VFS id-spaces never interleave.
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
