package main

import "testing"

func TestIsHeaderSource_ExtendedHeaderExtensions(t *testing.T) {
	for _, src := range []string{
		"a.h",
		"a.hh",
		"a.hpp",
		"a.hxx",
		"a.ipp",
		"a.ixx",
		"a.inl",
	} {
		if !isHeaderSource(src) {
			t.Fatalf("isHeaderSource(%q) = false, want true", src)
		}
	}
}
