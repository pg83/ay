package main

import "testing"

// TestStarlark_DeclareExternalResource pins the declare_external_resource() builtin and
// its host-bundle siblings: each forwards its positional arguments to a DeclareResourceStmt
// under the matching macro name.
func TestStarlark_DeclareExternalResource(t *testing.T) {
	env := DefaultIfEnv.clone()

	cases := []struct {
		builtin string
		macro   string
	}{
		{"declare_external_resource", "DECLARE_EXTERNAL_RESOURCE"},
		{"declare_external_host_resources_bundle", "DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE"},
		{"declare_external_host_resources_bundle_by_json", "DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON"},
	}

	for _, c := range cases {
		t.Run(c.macro, func(t *testing.T) {
			assertSameStmts(t,
				evalStarStr(t, `resources_library(extra_outputs = `+c.builtin+`("FOO_RESOURCE", "sbr:123", "FOR", "linux"))`, env),
				parseMakeStr(t, "RESOURCES_LIBRARY()\n"+c.macro+"(FOO_RESOURCE sbr:123 FOR linux)\nEND()\n"))
		})
	}
}
