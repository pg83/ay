package main

func appendInternStrs(dst []STR, ss []string) []STR {
	for _, s := range ss {
		dst = append(dst, internStr(s))
	}

	return dst
}

func appendStrStrs(dst []string, as []STR) []string {
	for _, a := range as {
		dst = append(dst, a.string())
	}

	return dst
}

func strStrs(as []STR) []string {
	return appendStrStrs(make([]string, 0, len(as)), as)
}

func sTRS(items ...string) []STR {
	out := make([]STR, 0, len(items))

	for _, s := range items {
		out = append(out, internStr(s))
	}

	return out
}
