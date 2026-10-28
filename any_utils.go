package main

import "strings"

func appendArgGroupStr(dst []ANY, srcs ...[]ANY) []ANY {
	for _, s := range srcs {
		for _, a := range s {
			for _, tok := range strings.Fields(a.string()) {
				dst = append(dst, internStr(tok).any())
			}
		}
	}

	return dst
}

func anyStrs(as []ANY) []string {
	out := make([]string, 0, len(as))

	for _, a := range as {
		out = append(out, a.string())
	}

	return out
}

func appendAnyLists(dst []ANY, srcs ...[]ANY) []ANY {
	for _, s := range srcs {
		dst = append(dst, s...)
	}

	return dst
}

func appendInternAnys(dst []ANY, ss []string) []ANY {
	for _, s := range ss {
		dst = append(dst, internStr(s).any())
	}

	return dst
}

func anysOf(items ...string) []ANY {
	return internAnys(items)
}

func internAnys(ss []string) []ANY {
	if len(ss) == 0 {
		return nil
	}

	out := make([]ANY, len(ss))

	for i, s := range ss {
		out[i] = internAny(s)
	}

	return out
}

func anyStrings(items []ANY) []string {
	out := make([]string, len(items))

	for i, item := range items {
		out[i] = item.string()
	}

	return out
}

func internAnyWords(args []ANY) STR {
	n := max(len(args)-1, 0)

	for _, arg := range args {
		rel := arg.relOrSelf().string()

		n += len(rel)

		if arg.vfs() != 0 {
			n += vfsPrefixLen

			if rel == "" {
				n--
			}
		}
	}

	block := internTable.bytes.alloc(n)
	k := 0

	for i, arg := range args {
		if i > 0 {
			block[k] = ' '
			k++
		}

		rel := arg.relOrSelf().string()

		if v := arg.vfs(); v != 0 {
			prefix := v.prefix()

			if rel == "" {
				prefix = prefix[:len(prefix)-1]
			}

			k += copy(block[k:], prefix)
		}

		k += copy(block[k:], rel)
	}

	return internBlock(block, k)
}
