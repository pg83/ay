package main

import "testing"

func TestParseCIncludes_IncludeNextNotMisparsed(t *testing.T) {
	block := make([]IncludeDirective, 64)
	got := block[:parseCIncludes([]byte("#if __has_include_next(<stdlib.h>)\n#    include_next <stdlib.h>\n#endif\n"), block, 0)]

	for _, d := range got {
		if d.target.string() == "_next" {
			t.Fatalf("#include_next misparsed as include %q; directives: %+v", d.target.string(), got)
		}
	}

	if len(got) != 0 {
		t.Fatalf("expected no directives from an #include_next block, got %+v", got)
	}

	nblock := make([]IncludeDirective, 64)
	norm := nblock[:parseCIncludes([]byte("#include <foo/bar.h>\n#include \"baz.h\"\n"), nblock, 0)]

	if len(norm) != 2 || norm[0].target.string() != "foo/bar.h" || norm[1].target.string() != "baz.h" {
		t.Fatalf("normal #include parsing regressed: %+v", norm)
	}
}
