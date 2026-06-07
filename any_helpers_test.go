package main

// anys builds an []ANY from string literals for CmdArgs test fixtures. Each element
// round-trips through ANY.String() back to the original string, so a fixture built
// with anys serializes identically to the former []string form.
func anys(ss ...string) []ANY { return appendStringAny(nil, ss) }
