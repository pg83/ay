package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestStripComments_BlockCommentInclude(t *testing.T) {
	in := []byte(`#include <real.h>
/*
 * Code used to generate the LUT.
 * #include <cmath>
 * #include <format>
 * #include <iostream>
 */
#include <other.h>`)

	out := stripComments(append([]byte(nil), in...))

	s := string(out)

	if !strings.Contains(s, "#include <real.h>") {
		t.Errorf("pre-comment #include lost: %q", s)
	}

	if !strings.Contains(s, "#include <other.h>") {
		t.Errorf("post-comment #include lost: %q", s)
	}

	for _, ghost := range []string{"<cmath>", "<format>", "<iostream>"} {
		if strings.Contains(s, ghost) {
			t.Errorf("ghost include %q survived block-comment strip; output:\n%s", ghost, s)
		}
	}

	if got, want := bytes.Count(out, []byte{'\n'}), bytes.Count(in, []byte{'\n'}); got != want {
		t.Errorf("newline count mismatch: got %d, want %d", got, want)
	}
}

func TestStripComments_LineCommentInclude(t *testing.T) {
	in := []byte(`#include <real.h> // and not #include <ghost.h>
int main() {} // unrelated #include <also-ghost.h>
`)

	out := stripComments(append([]byte(nil), in...))
	s := string(out)

	if !strings.Contains(s, "#include <real.h>") {
		t.Errorf("real include lost: %q", s)
	}

	if strings.Contains(s, "<ghost.h>") || strings.Contains(s, "<also-ghost.h>") {
		t.Errorf("line-comment ghost survived: %q", s)
	}
}

func TestStripComments_StringLiteralTransparent(t *testing.T) {
	in := []byte(`#include "syscall.h"
const char *msg = "this /* is not a comment */ end";
#include <real.h>
`)

	out := stripComments(append([]byte(nil), in...))
	s := string(out)

	if !strings.Contains(s, `#include "syscall.h"`) {
		t.Errorf("quoted #include lost: %q", s)
	}

	if !strings.Contains(s, "#include <real.h>") {
		t.Errorf("post-string #include lost: %q", s)
	}

	if !strings.Contains(s, "/* is not a comment */") {
		t.Errorf("string-literal contents modified or block-comment state entered: %q", s)
	}

	if got, want := bytes.Count(out, []byte{'\n'}), bytes.Count(in, []byte{'\n'}); got != want {
		t.Errorf("newline count mismatch: got %d, want %d", got, want)
	}
}

func TestStripComments_StringEscapedQuote(t *testing.T) {
	in := []byte(`const char *s = "a \" /* still in string */ end";
#include <real.h>
`)

	out := stripComments(append([]byte(nil), in...))
	s := string(out)

	if !strings.Contains(s, "#include <real.h>") {
		t.Errorf("post-string #include lost: %q", s)
	}

	if !strings.Contains(s, "/* still in string */") {
		t.Errorf("escape-quote handling broke: string body modified or comment state entered: %q", s)
	}
}

func TestStripComments_RawStringLiteral(t *testing.T) {
	in := []byte(`const char *s = R"py(
no escape needed: " or \ — /* not a real block comment */ done
)py";
#include <real.h>
`)

	out := stripComments(append([]byte(nil), in...))
	s := string(out)

	if !strings.Contains(s, "#include <real.h>") {
		t.Errorf("post-raw-string #include lost: %q", s)
	}

	if strings.Contains(s, "not a real block comment") {
		t.Errorf("raw-string body bytes survived; expected blanked: %q", s)
	}

	if got, want := bytes.Count(out, []byte{'\n'}), bytes.Count(in, []byte{'\n'}); got != want {
		t.Errorf("newline count mismatch: got %d, want %d", got, want)
	}
}

func TestStripComments_RawStringFakeIncludeBlanked(t *testing.T) {
	in := []byte(`p->Emit(R"(
#include "$path$"
)");
#include <real.h>
`)

	out := stripComments(append([]byte(nil), in...))
	s := string(out)

	if !strings.Contains(s, "#include <real.h>") {
		t.Errorf("post-raw-string #include lost: %q", s)
	}

	if strings.Contains(s, `#include "$path$"`) {
		t.Errorf("raw-string fake #include survived blanking: %q", s)
	}
}

func TestStripComments_QuotedIncludeSurvives(t *testing.T) {
	in := []byte("#include <sys/sendfile.h>\n#include \"syscall.h\"\n")

	out := stripComments(append([]byte(nil), in...))

	if string(out) != string(in) {
		t.Errorf("quoted include altered:\n got: %q\nwant: %q", out, in)
	}
}

func TestStripComments_NestedBlockCommentNotAttempted(t *testing.T) {
	in := []byte(`/* outer /* inner */ rest */`)

	out := stripComments(append([]byte(nil), in...))
	s := string(out)

	if !strings.Contains(s, "rest") {
		t.Errorf("post-first-`*/` code lost: %q", s)
	}

	if strings.Contains(s, "outer") || strings.Contains(s, "inner") {
		t.Errorf("inside-comment text survived: %q", s)
	}
}

func TestStripComments_NoTriggers(t *testing.T) {
	in := []byte("#include <abc>\n#include <def>\n")
	out := stripComments(in)

	if &in[0] != &out[0] {
		t.Errorf("trigger-free fast path allocated: in=%p out=%p", &in[0], &out[0])
	}
}

func TestStripComments_DivisionOperatorNotMistaken(t *testing.T) {
	in := []byte("int x = a / b;\n#include <real.h>\n")
	out := stripComments(append([]byte(nil), in...))

	if string(out) != string(in) {
		t.Errorf("division operator misread as comment:\n got: %q\nwant: %q", out, in)
	}
}

func TestScanner_SearchTierCacheReuse_OwnAddIncl(t *testing.T) {
	fs := newMemFS(map[string]string{
		"include/foo.h": "// foo\n",
	})

	scanner := newTestScanner(fs, nil)
	sc := scanner.NewScanCtx(ScanContext{
		OwnAddIncl: VFSesFromStrings([]string{"include"}),
	})
	d := includeDirective{kind: includeSystem, target: internString("foo.h")}

	got1 := sc.resolveSearchPath(Intern("$(S)/pkg/a.cpp"), d)
	got2 := sc.resolveSearchPath(Intern("$(S)/pkg/b.cpp"), d)
	want := []VFS{Intern("$(S)/include/foo.h")}

	if len(got1) != len(want) || got1[0] != want[0] {
		t.Fatalf("first resolve = %v, want %v", got1, want)
	}

	if len(got2) != len(want) || got2[0] != want[0] {
		t.Fatalf("second resolve = %v, want %v", got2, want)
	}

	if len(sc.searchTierCache) != 1 {
		t.Fatalf("searchTierCache entries = %d, want 1 (shared by target)", len(sc.searchTierCache))
	}

	if scanner.searchTierMisses != 1 || scanner.searchTierHits != 1 {
		t.Fatalf("searchTier hits/misses = %d/%d, want 1/1", scanner.searchTierHits, scanner.searchTierMisses)
	}
}

func TestScanner_SearchTierCacheReuse_NotFound(t *testing.T) {
	scanner := newTestScanner(newMemFS(nil), nil)
	sc := scanner.NewScanCtx(ScanContext{
		OwnAddIncl: VFSesFromStrings([]string{"include"}),
	})
	d := includeDirective{kind: includeSystem, target: internString("missing.h")}

	got1 := sc.resolveSearchPath(Intern("$(S)/pkg/a.cpp"), d)
	got2 := sc.resolveSearchPath(Intern("$(S)/pkg/b.cpp"), d)

	if got1 != nil || got2 != nil {
		t.Fatalf("missing header resolved unexpectedly: first=%v second=%v", got1, got2)
	}

	if len(sc.searchTierCache) != 1 {
		t.Fatalf("searchTierCache entries = %d, want 1", len(sc.searchTierCache))
	}

	if sc.searchTierCache[internString("missing.h")].found {
		t.Fatalf("missing header cached as found")
	}

	if scanner.searchTierMisses != 1 || scanner.searchTierHits != 1 {
		t.Fatalf("searchTier hits/misses = %d/%d, want 1/1", scanner.searchTierHits, scanner.searchTierMisses)
	}
}

func TestScanner_SearchTierCacheBypassedBySameDirQuoted(t *testing.T) {
	fs := newMemFS(map[string]string{
		"pkg/foo.h":     "// local\n",
		"include/foo.h": "// addincl\n",
	})

	scanner := newTestScanner(fs, nil)
	sc := scanner.NewScanCtx(ScanContext{
		OwnAddIncl: VFSesFromStrings([]string{"include"}),
	})
	got := sc.resolveSearchPath(Intern("$(S)/pkg/a.cpp"), includeDirective{kind: includeQuoted, target: internString("foo.h")})
	want := []VFS{Intern("$(S)/pkg/foo.h")}

	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("quoted same-dir resolve = %v, want %v", got, want)
	}

	if len(sc.searchTierCache) != 0 {
		t.Fatalf("searchTierCache entries = %d, want 0 when same-dir quoted wins", len(sc.searchTierCache))
	}

	if scanner.searchTierHits != 0 || scanner.searchTierMisses != 0 {
		t.Fatalf("searchTier hits/misses = %d/%d, want 0/0", scanner.searchTierHits, scanner.searchTierMisses)
	}
}

func TestScanner_QuotedSysinclGated_LocalResolved(t *testing.T) {
	fs := newMemFS(map[string]string{
		"yasm/source.cpp":         "#include \"elf.h\"\n",
		"yasm/elf.h":              "// local elf.h\n",
		"foolib/include/elf.h":    "// foolib elf.h\n",
	})

	sysincl := SysInclSet{
		{
			Filter:      nil,
			KeyBySource: false,
			Mappings: map[string][]string{
				"elf.h": {"foolib/include/elf.h"},
			},
		},
	}

	scanner := newTestScanner(fs, sysincl)
	closure := scanner.WalkClosure(ScanContext{
		SourceRel: "yasm/source.cpp",
	})

	hasLocal := false
	hasFoo := false

	for _, p := range closure {
		switch {
		case strings.HasSuffix(p.String(), "/yasm/elf.h"):
			hasLocal = true
		case strings.HasSuffix(p.String(), "/foolib/include/elf.h"):
			hasFoo = true
		}
	}

	if !hasLocal {
		t.Errorf("closure missing local yasm/elf.h (search-path resolution broken): %v", closure)
	}

	if hasFoo {
		t.Errorf("closure contains spurious foolib/include/elf.h — PR-35w gate failed to suppress sysincl on locally-resolved quoted include: %v", closure)
	}
}

func TestScanner_QuotedMultiTargetSysincl_OwnAddIncl(t *testing.T) {
	fs := newMemFS(map[string]string{
		"src/header.h":                  "#include \"cxxabi.h\"\n",
		"src/source.cpp":                "#include \"header.h\"\n",
		"libcxxabi/include/cxxabi.h":    "// libcxxabi cxxabi.h\n",
		"libcxxrt/include/cxxabi.h":     "// libcxxrt cxxabi.h\n",
	})

	sysincl := SysInclSet{
		{
			Filter:         nil,
			KeyBySource:    false,
			HasMultiTarget: true,
			Mappings: map[string][]string{
				"cxxabi.h": {
					"libcxxabi/include/cxxabi.h",
					"libcxxrt/include/cxxabi.h",
				},
			},
		},
	}

	scanner := newTestScanner(fs, sysincl)
	closure := scanner.WalkClosure(ScanContext{
		SourceRel:  "src/source.cpp",
		OwnAddIncl: VFSesFromStrings([]string{"libcxxabi/include"}),
	})

	hasLibcxxabi := false
	hasLibcxxrt := false

	for _, p := range closure {
		switch {
		case strings.HasSuffix(p.String(), "/libcxxabi/include/cxxabi.h"):
			hasLibcxxabi = true
		case strings.HasSuffix(p.String(), "/libcxxrt/include/cxxabi.h"):
			hasLibcxxrt = true
		}
	}

	if !hasLibcxxabi {
		t.Errorf("closure missing libcxxabi/include/cxxabi.h (OwnAddIncl resolution broken): %v", closure)
	}

	if !hasLibcxxrt {
		t.Errorf("closure missing libcxxrt/include/cxxabi.h — PR-36 multi-target bypass failed "+
			"for OwnAddIncl-resolved quoted include: %v", closure)
	}
}

func TestScanner_QuotedSameDirStillGated(t *testing.T) {
	fs := newMemFS(map[string]string{
		"libcxxrt/dwarf_eh.h":      "#include \"unwind.h\"\n",
		"libcxxrt/source.cc":       "#include \"dwarf_eh.h\"\n",
		"libcxxrt/unwind.h":        "// libcxxrt unwind.h\n",
		"libcxx/include/unwind.h":  "// libcxx unwind.h\n",
	})

	sysincl := SysInclSet{
		{
			Filter:         nil,
			KeyBySource:    false,
			HasMultiTarget: true,
			Mappings: map[string][]string{
				"unwind.h": {
					"libcxx/include/unwind.h",
					"libcxxrt/unwind.h",
				},
			},
		},
	}

	scanner := newTestScanner(fs, sysincl)
	closure := scanner.WalkClosure(ScanContext{
		SourceRel:  "libcxxrt/source.cc",
		OwnAddIncl: VFSesFromStrings([]string{"libcxxrt"}),
	})

	hasLibcxxrt := false
	hasLibcxxSpurious := false

	for _, p := range closure {
		switch {
		case strings.HasSuffix(p.String(), "/libcxxrt/unwind.h"):
			hasLibcxxrt = true
		case strings.HasSuffix(p.String(), "/libcxx/include/unwind.h"):
			hasLibcxxSpurious = true
		}
	}

	if !hasLibcxxrt {
		t.Errorf("closure missing local libcxxrt/unwind.h (same-dir resolution broken): %v", closure)
	}

	if hasLibcxxSpurious {
		t.Errorf("closure contains spurious libcxx/include/unwind.h — PR-36 same-dir gate failed "+
			"(must NOT bypass for same-dir resolved quoted includes): %v", closure)
	}
}

func TestScanner_QuotedSysinclFiresOnLocalMiss(t *testing.T) {
	fs := newMemFS(map[string]string{
		"src/source.cpp":            "#include \"absent.h\"\n",
		"foolib/include/absent.h":   "// foolib absent.h\n",
	})

	sysincl := SysInclSet{
		{
			Filter:      nil,
			KeyBySource: false,
			Mappings: map[string][]string{
				"absent.h": {"foolib/include/absent.h"},
			},
		},
	}

	scanner := newTestScanner(fs, sysincl)
	closure := scanner.WalkClosure(ScanContext{
		SourceRel: "src/source.cpp",
	})

	hasFoolib := false

	for _, p := range closure {
		if strings.HasSuffix(p.String(), "/foolib/include/absent.h") {
			hasFoolib = true

			break
		}
	}

	if !hasFoolib {
		t.Errorf("closure missing foolib/include/absent.h — gate over-suppresses sysincl when local resolution failed: %v", closure)
	}
}

func TestScanner_AngleSysinclUnaffected(t *testing.T) {
	fs := newMemFS(map[string]string{
		"libcxxrt/source.cpp":          "#include <unwind.h>\n",
		"libcxxrt/unwind.h":            "// libcxxrt unwind.h\n",
		"libunwind/include/unwind.h":   "// libunwind unwind.h\n",
	})

	sysincl := SysInclSet{
		{
			Filter:      nil,
			KeyBySource: false,
			Mappings: map[string][]string{
				"unwind.h": {"libunwind/include/unwind.h"},
			},
		},
	}

	scanner := newTestScanner(fs, sysincl)
	closure := scanner.WalkClosure(ScanContext{
		SourceRel:  "libcxxrt/source.cpp",
		OwnAddIncl: VFSesFromStrings([]string{"libcxxrt"}),
	})

	hasLocal := false
	hasLibunwind := false

	for _, p := range closure {
		switch {
		case strings.HasSuffix(p.String(), "/libcxxrt/unwind.h"):
			hasLocal = true
		case strings.HasSuffix(p.String(), "/libunwind/include/unwind.h"):
			hasLibunwind = true
		}
	}

	if !hasLocal {
		t.Errorf("closure missing local libcxxrt/unwind.h: %v", closure)
	}

	if !hasLibunwind {
		t.Errorf("closure missing libunwind/include/unwind.h — PR-35w gate over-suppressed sysincl on angle-bracket include: %v", closure)
	}
}

func TestParseYasmIncludes_LowercaseQuoted(t *testing.T) {
	in := []byte(`%include "defs.asm"
some_label:
    mov rax, 0
`)

	dirs := parseYasmIncludes(in)

	if len(dirs) != 1 {
		t.Fatalf("got %d directives, want 1; %+v", len(dirs), dirs)
	}

	if dirs[0].target.String() != "defs.asm" {
		t.Errorf("target = %q, want %q", dirs[0].target.String(), "defs.asm")
	}

	if dirs[0].kind != includeQuoted {
		t.Errorf("kind = %v, want includeQuoted", dirs[0].kind)
	}

	if dirs[0].next {
		t.Errorf("next = true, want false (yasm has no %%include_next)")
	}
}

func TestParseYasmIncludes_UppercaseDirective(t *testing.T) {
	in := []byte(`%INCLUDE "randomah.asi"
`)

	dirs := parseYasmIncludes(in)

	if len(dirs) != 1 {
		t.Fatalf("got %d directives, want 1; %+v", len(dirs), dirs)
	}

	if dirs[0].target.String() != "randomah.asi" {
		t.Errorf("target = %q, want %q", dirs[0].target.String(), "randomah.asi")
	}

	if dirs[0].kind != includeQuoted {
		t.Errorf("kind = %v, want includeQuoted", dirs[0].kind)
	}
}

func TestParseYasmIncludes_LineCommentIgnored(t *testing.T) {
	in := []byte(`; %include "ghost.asm"
%include "real.asm"
`)

	dirs := parseYasmIncludes(in)

	if len(dirs) != 1 {
		t.Fatalf("got %d directives, want 1; %+v", len(dirs), dirs)
	}

	if dirs[0].target.String() != "real.asm" {
		t.Errorf("target = %q, want %q", dirs[0].target.String(), "real.asm")
	}
}

func TestParseYasmIncludes_TrailingSemicolonComment(t *testing.T) {
	in := []byte(`%include "instrset64.asm"              ; include code for InstructionSet function
`)

	dirs := parseYasmIncludes(in)

	if len(dirs) != 1 {
		t.Fatalf("got %d directives, want 1; %+v", len(dirs), dirs)
	}

	if dirs[0].target.String() != "instrset64.asm" {
		t.Errorf("target = %q, want %q", dirs[0].target.String(), "instrset64.asm")
	}
}

func TestParseYasmIncludes_NoMatchOnCInclude(t *testing.T) {
	in := []byte(`#include "foo.h"
`)

	dirs := parseYasmIncludes(in)

	if len(dirs) != 0 {
		t.Errorf("got %d directives, want 0; %+v", len(dirs), dirs)
	}
}

func TestParseYasmIncludes_AngleBracketForm(t *testing.T) {
	in := []byte(`%include <sysmacros.asi>
`)

	dirs := parseYasmIncludes(in)

	if len(dirs) != 1 {
		t.Fatalf("got %d directives, want 1; %+v", len(dirs), dirs)
	}

	if dirs[0].target.String() != "sysmacros.asi" {
		t.Errorf("target = %q, want %q", dirs[0].target.String(), "sysmacros.asi")
	}

	if dirs[0].kind != includeSystem {
		t.Errorf("kind = %v, want includeSystem", dirs[0].kind)
	}
}

func TestScanDirectives_ProtoImports(t *testing.T) {
	scanner := newTestScanner(newMemFS(map[string]string{
		"src.proto": `import "a.proto";
import public "b.proto";
import weak 'c.proto';
// import "ghost.proto";
`,
	}), SysInclSet{})
	dirs := scanner.sourceParsedBuckets(Intern("$(S)/src.proto")).bucket(parsedIncludesLocal)

	if len(dirs) != 3 {
		t.Fatalf("got %d directives, want 3; %+v", len(dirs), dirs)
	}

	got := []string{dirs[0].target.String(), dirs[1].target.String(), dirs[2].target.String()}
	want := []string{"a.proto", "b.proto", "c.proto"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("targets = %v, want %v", got, want)
		}
		if dirs[i].kind != includeQuoted {
			t.Fatalf("dirs[%d].kind = %v, want includeQuoted", i, dirs[i].kind)
		}
	}
}

func TestParsedIncludes_ProtoBuckets(t *testing.T) {
	scanner := newTestScanner(newMemFS(map[string]string{
		"src.proto": `import "a.proto";
import public "b.proto";
import weak "c.proto";
import public "d.ev";
`,
	}), SysInclSet{})
	parsed := scanner.parsedIncludes(Intern("$(S)/src.proto"))
	local := parsed.bucket(parsedIncludesLocal)
	hcpp := parsed.bucket(parsedIncludesHCPP)

	if len(local) != 4 {
		t.Fatalf("got %d local entries, want 4; %+v", len(local), local)
	}
	if len(hcpp) != 4 {
		t.Fatalf("got %d h+cpp entries, want 4; %+v", len(hcpp), hcpp)
	}

	wantHCPP := []string{"a.pb.h", "b.pb.h", "c.pb.h", "d.ev.pb.h"}
	for i, want := range wantHCPP {
		if hcpp[i].target.String() != want {
			t.Fatalf("hcpp[%d].target.String() = %q, want %q; all=%+v", i, hcpp[i].target.String(), want, hcpp)
		}
		if hcpp[i].kind != includeQuoted {
			t.Fatalf("hcpp[%d].kind = %v, want includeQuoted", i, hcpp[i].kind)
		}
	}
}

func TestParsedIncludes_RagelBuckets(t *testing.T) {
	scanner := newTestScanner(newMemFS(map[string]string{
		"src.rl6": `#include <outer.h>
%%{
include "machine.rl";
include Foo "machine2.rl";
include "machine.rl";
}%%
#include "tail.h"
`,
	}), SysInclSet{})
	parsed := scanner.parsedIncludes(Intern("$(S)/src.rl6"))
	local := parsed.bucket(parsedIncludesLocal)
	hcpp := parsed.bucket(parsedIncludesHCPP)

	if len(local) != 4 {
		t.Fatalf("got %d local entries, want 4; %+v", len(local), local)
	}

	wantLocalTargets := []string{"outer.h", "tail.h", "machine.rl", "machine2.rl"}
	wantLocalKinds := []includeKind{includeSystem, includeQuoted, includeQuoted, includeQuoted}
	for i := range wantLocalTargets {
		if local[i].target.String() != wantLocalTargets[i] {
			t.Fatalf("local[%d].target.String() = %q, want %q; all=%+v", i, local[i].target.String(), wantLocalTargets[i], local)
		}
		if local[i].kind != wantLocalKinds[i] {
			t.Fatalf("local[%d].kind = %v, want %v", i, local[i].kind, wantLocalKinds[i])
		}
	}

	if len(hcpp) != 1 {
		t.Fatalf("got %d h+cpp entries, want 1; %+v", len(hcpp), hcpp)
	}
	if hcpp[0].target.String() != "src.rl6" || hcpp[0].kind != includeQuoted {
		t.Fatalf("h+cpp = %+v, want quoted self target \"src.rl6\"", hcpp)
	}
}

func TestParsedIncludes_LeadingUTF8BOM(t *testing.T) {
	scanner := newTestScanner(newMemFS(map[string]string{
		"bom.cpp": "\xef\xbb\xbf#include \"sibling.h\"\n#include <system.h>\n",
	}), SysInclSet{})
	local := scanner.parsedIncludes(Intern("$(S)/bom.cpp")).bucket(parsedIncludesLocal)

	if len(local) != 2 {
		t.Fatalf("got %d local entries, want 2 (BOM not stripped?); %+v", len(local), local)
	}
	if local[0].target.String() != "sibling.h" || local[0].kind != includeQuoted {
		t.Fatalf("local[0] = %+v, want quoted \"sibling.h\"", local[0])
	}
}

func TestParsedIncludes_SwigBuckets(t *testing.T) {
	scanner := newTestScanner(newMemFS(map[string]string{
		"src.swg": `%module x
%include "a.i"
%import <b.i>
%insert(runtime) "c.h"
%{
#include "block.h"
%}
`,
	}), SysInclSet{})
	parsed := scanner.parsedIncludes(Intern("$(S)/src.swg"))
	local := parsed.bucket(parsedIncludesLocal)
	hcpp := parsed.bucket(parsedIncludesHCPP)

	if len(local) != 3 {
		t.Fatalf("got %d local entries, want 3; %+v", len(local), local)
	}

	wantLocalTargets := []string{"a.i", "b.i", "c.h"}
	wantLocalKinds := []includeKind{includeQuoted, includeSystem, includeQuoted}
	for i := range wantLocalTargets {
		if local[i].target.String() != wantLocalTargets[i] {
			t.Fatalf("local[%d].target.String() = %q, want %q; all=%+v", i, local[i].target.String(), wantLocalTargets[i], local)
		}
		if local[i].kind != wantLocalKinds[i] {
			t.Fatalf("local[%d].kind = %v, want %v", i, local[i].kind, wantLocalKinds[i])
		}
	}

	if len(hcpp) != 1 {
		t.Fatalf("got %d h+cpp entries, want 1; %+v", len(hcpp), hcpp)
	}
	if hcpp[0].target.String() != "block.h" || hcpp[0].kind != includeQuoted {
		t.Fatalf("h+cpp = %+v, want block.h quoted", hcpp)
	}
}

func TestScanDirectives_DispatchByExtension(t *testing.T) {
	scanner := newTestScanner(newMemFS(map[string]string{
		"src.asm": "%include \"defs.asm\"\n#include \"should-not-match.h\"\n",
		"src.h":   "#include \"real.h\"\n%include \"should-not-match.asm\"\n",
	}), SysInclSet{})

	asmDirs := scanner.scanDirectives(Intern("$(S)/src.asm"))
	hDirs := scanner.scanDirectives(Intern("$(S)/src.h"))

	if len(asmDirs) != 1 || asmDirs[0].target.String() != "defs.asm" {
		t.Errorf("asm dispatch failed: got %+v, want one directive targeting defs.asm", asmDirs)
	}

	if len(hDirs) != 1 || hDirs[0].target.String() != "real.h" {
		t.Errorf("h dispatch failed: got %+v, want one directive targeting real.h", hDirs)
	}
}

func TestScanDirectives_AsiDispatchesToYasm(t *testing.T) {
	scanner := newTestScanner(newMemFS(map[string]string{
		"src.asi": "%include \"nested.asi\"\n",
	}), SysInclSet{})

	dirs := scanner.scanDirectives(Intern("$(S)/src.asi"))

	if len(dirs) != 1 || dirs[0].target.String() != "nested.asi" {
		t.Errorf(".asi dispatch failed: got %+v, want one directive targeting nested.asi", dirs)
	}
}

func TestScanDirectives_G4UsesEmptyParser(t *testing.T) {
	scanner := newTestScanner(newMemFS(map[string]string{
		"src.g4": "#include \"ghost.h\"\ngrammar X;\n",
	}), SysInclSet{})
	dirs := scanner.scanDirectives(Intern("$(S)/src.g4"))

	if len(dirs) != 0 {
		t.Errorf(".g4 should use empty parser, got %+v", dirs)
	}
}

func TestScanDirectives_InSuffixUsesUnderlyingExtension(t *testing.T) {
	scanner := newTestScanner(newMemFS(map[string]string{
		"src.g4.in": "#include \"ghost.h\"\ngrammar X;\n",
	}), SysInclSet{})
	dirs := scanner.scanDirectives(Intern("$(S)/src.g4.in"))

	if len(dirs) != 0 {
		t.Errorf(".g4.in should inherit .g4 empty parser, got %+v", dirs)
	}
}

func TestScanDirectives_MacroIndirectAugmentation(t *testing.T) {
	const rel = "contrib/libs/openssl/crypto/uid.c"

	scanner := newTestScanner(newMemFS(map[string]string{
		rel: "#include <openssl/crypto.h>\n# include OPENSSL_UNISTD\n",
	}), SysInclSet{})
	dirs := scanner.scanDirectives(Source(rel))

	var hasCrypto, hasUnistd bool

	for _, d := range dirs {
		if d.target.String() == "openssl/crypto.h" && d.kind == includeSystem {
			hasCrypto = true
		}

		if d.target.String() == "unistd.h" && d.kind == includeSystem {
			hasUnistd = true
		}
	}

	if !hasCrypto {
		t.Errorf("regex-parsed openssl/crypto.h missing: %+v", dirs)
	}

	if !hasUnistd {
		t.Errorf("macro-indirect unistd.h augmentation missing for openssl/crypto/uid.c: %+v", dirs)
	}
}

func (s *IncludeScanner) scanDirectives(vfsPath VFS) []includeDirective {
	return s.parsers.parsedIncludes(vfsPath)
}

func (s *IncludeScanner) parsedIncludes(vfsPath VFS) parsedIncludeSet {
	return s.parsers.sourceParsedBuckets(vfsPath)
}

func (s *IncludeScanner) sourceParsedBuckets(vfsPath VFS) parsedIncludeSet {
	return s.parsers.sourceParsedBuckets(vfsPath)
}

func assertHasIncludeDirective(t *testing.T, got []includeDirective, want includeDirective) {
	t.Helper()

	for _, d := range got {
		if d == want {
			return
		}
	}

	t.Fatalf("parsed includes missing %+v; got=%+v", want, got)
}

func assertLacksIncludeDirective(t *testing.T, got []includeDirective, want includeDirective) {
	t.Helper()

	for _, d := range got {
		if d == want {
			t.Fatalf("parsed includes unexpectedly contain %+v; got=%+v", want, got)
		}
	}
}

func assertHasVFS(t *testing.T, closure []VFS, want VFS) {
	t.Helper()

	if !containsVFS(closure, want) {
		t.Fatalf("closure missing %v: %v", want, closure)
	}
}

func assertLacksVFS(t *testing.T, closure []VFS, want VFS) {
	t.Helper()

	if containsVFS(closure, want) {
		t.Fatalf("closure unexpectedly contains %v: %v", want, closure)
	}
}

func TestScanner_CythonNestedPxdUsesPy2StringSibling(t *testing.T) {
	scanner := newTestScanner(newMemFS(map[string]string{
		"pkg/mod.pyx":             "from util.generic.string cimport TString\n",
		"util/generic/string.pxd": "from libcpp.string cimport string as _std_string\n",
		"contrib/tools/cython/Cython/Includes/libcpp/string.pxd":      "from libcpp.string_view cimport string_view\nfrom libc.string cimport memcpy\n",
		"contrib/tools/cython/Cython/Includes/libcpp/string_view.pxd": "# py3 string_view\n",
		"contrib/tools/cython/Cython/Includes/libc/string.pxd":        "# py3 libc string\n",
		"contrib/tools/cython_py2/Cython/Includes/libcpp/string.pxd":  "from libc.string cimport memcpy\n",
		"contrib/tools/cython_py2/Cython/Includes/libc/string.pxd":    "# py2 libc string\n",
	}), SysInclSet{})
	closure := scanner.WalkClosure(ScanContext{
		SourceRel: "pkg/mod.pyx",
		OwnAddIncl: []VFS{
			Intern("$(S)/"),
			Intern("$(S)/contrib/tools/cython/Cython/Includes"),
		},
	})

	assertHasVFS(t, closure, Intern("$(S)/util/generic/string.pxd"))
	assertHasVFS(t, closure, Intern("$(S)/contrib/tools/cython_py2/Cython/Includes/libcpp/string.pxd"))
	assertHasVFS(t, closure, Intern("$(S)/contrib/tools/cython_py2/Cython/Includes/libc/string.pxd"))
	assertLacksVFS(t, closure, Intern("$(S)/contrib/tools/cython/Cython/Includes/libcpp/string.pxd"))
	assertLacksVFS(t, closure, Intern("$(S)/contrib/tools/cython/Cython/Includes/libc/string.pxd"))
	assertLacksVFS(t, closure, Intern("$(S)/contrib/tools/cython/Cython/Includes/libcpp/string_view.pxd"))
}

func TestScanner_CythonPyxDirectStdlibStaysPy3WhileNestedPxdAddsPy2(t *testing.T) {
	scanner := newTestScanner(newMemFS(map[string]string{
		"pkg/mod.pyx":           "from libcpp.pair cimport pair\nfrom util.generic.hash cimport THashMap\n",
		"util/generic/hash.pxd": "from libcpp.pair cimport pair\n",
		"contrib/tools/cython/Cython/Includes/libcpp/pair.pxd":        "from libcpp.utility cimport move\n",
		"contrib/tools/cython/Cython/Includes/libcpp/utility.pxd":     "# py3 utility\n",
		"contrib/tools/cython_py2/Cython/Includes/libcpp/pair.pxd":    "from libcpp.utility cimport move\n",
		"contrib/tools/cython_py2/Cython/Includes/libcpp/utility.pxd": "# py2 utility\n",
	}), SysInclSet{})
	closure := scanner.WalkClosure(ScanContext{
		SourceRel: "pkg/mod.pyx",
		OwnAddIncl: []VFS{
			Intern("$(S)/"),
			Intern("$(S)/contrib/tools/cython/Cython/Includes"),
		},
	})

	assertHasVFS(t, closure, Intern("$(S)/contrib/tools/cython/Cython/Includes/libcpp/pair.pxd"))
	assertHasVFS(t, closure, Intern("$(S)/contrib/tools/cython/Cython/Includes/libcpp/utility.pxd"))
	assertHasVFS(t, closure, Intern("$(S)/contrib/tools/cython_py2/Cython/Includes/libcpp/pair.pxd"))
	assertHasVFS(t, closure, Intern("$(S)/contrib/tools/cython_py2/Cython/Includes/libcpp/utility.pxd"))
}

func TestScanner_CythonStdintSplitKeepsPy3InitButAddsPy2Types(t *testing.T) {
	scanner := newTestScanner(newMemFS(map[string]string{
		"pkg/mod.pyx":            "from util.datetime.base cimport TInstant\nfrom util.system.types cimport ui64\n",
		"util/datetime/base.pxd": "from libc.stdint cimport uint64_t\nfrom libcpp cimport bool as bool_t\n",
		"util/system/types.pxd":  "from libc.stdint cimport uint64_t\n",
		"contrib/tools/cython/Cython/Includes/libcpp/__init__.pxd":     "# py3 libcpp init\n",
		"contrib/tools/cython_py2/Cython/Includes/libcpp/__init__.pxd": "# py2 libcpp init\n",
		"contrib/tools/cython/Cython/Includes/libc/stdint.pxd":         "# py3 stdint\n",
		"contrib/tools/cython_py2/Cython/Includes/libc/stdint.pxd":     "# py2 stdint\n",
	}), SysInclSet{})
	closure := scanner.WalkClosure(ScanContext{
		SourceRel: "pkg/mod.pyx",
		OwnAddIncl: []VFS{
			Intern("$(S)/"),
			Intern("$(S)/contrib/tools/cython/Cython/Includes"),
		},
	})

	assertHasVFS(t, closure, Intern("$(S)/contrib/tools/cython/Cython/Includes/libcpp/__init__.pxd"))
	assertLacksVFS(t, closure, Intern("$(S)/contrib/tools/cython_py2/Cython/Includes/libcpp/__init__.pxd"))
	assertHasVFS(t, closure, Intern("$(S)/contrib/tools/cython/Cython/Includes/libc/stdint.pxd"))
	assertHasVFS(t, closure, Intern("$(S)/contrib/tools/cython_py2/Cython/Includes/libc/stdint.pxd"))
}

func hyperscanPeerAddIncl() []VFS {
	return VFSesFromStrings([]string{
		"contrib/libs/cxxsupp/libcxx/include",
		"contrib/libs/cxxsupp/libcxxrt/include",
		"contrib/libs/clang20-rt/include",
		"contrib/restricted/boost/dynamic_bitset/include",
		"contrib/restricted/boost/assert/include",
		"contrib/restricted/boost/config/include",
		"contrib/restricted/boost/container_hash/include",
		"contrib/restricted/boost/describe/include",
		"contrib/restricted/boost/mp11/include",
		"contrib/restricted/boost/core/include",
		"contrib/restricted/boost/throw_exception/include",
		"contrib/restricted/boost/graph/include",
		"contrib/restricted/boost/algorithm/include",
		"contrib/restricted/boost/array/include",
		"contrib/restricted/boost/bind/include",
		"contrib/restricted/boost/concept_check/include",
		"contrib/restricted/boost/preprocessor/include",
		"contrib/restricted/boost/type_traits/include",
		"contrib/restricted/boost/exception/include",
		"contrib/restricted/boost/smart_ptr/include",
		"contrib/restricted/boost/tuple/include",
		"contrib/restricted/boost/function/include",
		"contrib/restricted/boost/iterator/include",
		"contrib/restricted/boost/detail/include",
		"contrib/restricted/boost/fusion/include",
		"contrib/restricted/boost/function_types/include",
		"contrib/restricted/boost/mpl/include",
		"contrib/restricted/boost/predef/include",
		"contrib/restricted/boost/utility/include",
		"contrib/restricted/boost/io/include",
		"contrib/restricted/boost/functional/include",
		"contrib/restricted/boost/typeof/include",
		"contrib/restricted/boost/optional/include",
		"contrib/restricted/boost/range/include",
		"contrib/restricted/boost/conversion/include",
		"contrib/restricted/boost/regex/include",
		"contrib/libs/icu/include",
		"contrib/restricted/boost/unordered/include",
		"contrib/restricted/boost/container/include",
		"contrib/restricted/boost/intrusive/include",
		"contrib/restricted/boost/move/include",
		"contrib/restricted/boost/any/include",
		"contrib/restricted/boost/type_index/include",
		"contrib/restricted/boost/bimap/include",
		"contrib/restricted/boost/lambda/include",
		"contrib/restricted/boost/multi_index/include",
		"contrib/restricted/boost/integer/include",
		"contrib/restricted/boost/static_assert/include",
		"contrib/restricted/boost/lexical_cast/include",
		"contrib/restricted/boost/math/include",
		"contrib/restricted/boost/random/include",
		"contrib/restricted/boost/system/include",
		"contrib/restricted/boost/variant2/include",
		"contrib/restricted/boost/winapi/include",
		"contrib/restricted/boost/parameter/include",
		"contrib/restricted/boost/property_map/include",
		"contrib/restricted/boost/property_tree/include",
		"contrib/restricted/boost/serialization/include",
		"contrib/restricted/boost/spirit/include",
		"contrib/restricted/boost/endian/include",
		"contrib/restricted/boost/phoenix/include",
		"contrib/restricted/boost/proto/include",
		"contrib/restricted/boost/pool/include",
		"contrib/restricted/boost/thread/include",
		"contrib/restricted/boost/atomic/include",
		"contrib/restricted/boost/align/include",
		"contrib/restricted/boost/chrono/include",
		"contrib/restricted/boost/ratio/include",
		"contrib/restricted/boost/date_time/include",
		"contrib/restricted/boost/numeric_conversion/include",
		"contrib/restricted/boost/tokenizer/include",
		"contrib/restricted/boost/variant/include",
		"contrib/restricted/boost/tti/include",
		"contrib/restricted/boost/xpressive/include",
		"contrib/restricted/boost/icl/include",
		"contrib/restricted/boost/rational/include",
		"contrib/restricted/boost/multi_array/include",
	})
}

// attachCodegen wires a CodegenRegistry onto an existing scanner the way the
// real gen pipeline does (codegen field + codegenLocator fallback). Tests use
// it together with newTestScanner.
func attachCodegen(scanner *IncludeScanner, reg *CodegenRegistry) {
	scanner.codegen = reg
	scanner.fallbackLocators = []pathLocator{codegenLocator{reg: reg}}
}

// TestScanner_AddInclBuildBeforeSourceWinsWhenBothExist locks the upstream
// resolve order: when ADDINCL declares a Build-rooted prefix BEFORE a
// Source-rooted prefix and the target header exists under both (committed
// source stub + codegen-registered generated output), upstream returns the
// Build path because IncDirs are iterated in declaration order with first
// match wins (devtools/ymake/module_resolver.cpp:371 → resolver/path_resolver
// CheckByRoot). Real-world repro of the OMP.inc case in contrib/libs/llvm16.
func TestScanner_AddInclBuildBeforeSourceWinsWhenBothExist(t *testing.T) {
	reg := NewCodegenRegistry()
	reg.Register(&GeneratedFileInfo{
		ProducerKvP: "PR",
		OutputPath:  Build("contrib/libs/llvm16/include/llvm/Frontend/OpenMP/OMP.inc"),
	})

	fs := newMemFS(map[string]string{
		"contrib/libs/llvm16/include/llvm/Frontend/OpenMP/OMP.inc": "// committed stub\n",
	})
	scanner := newTestScanner(fs, nil)
	attachCodegen(scanner, reg)
	sc := scanner.NewScanCtx(ScanContext{
		PeerAddInclSet: []VFS{
			Build("contrib/libs/llvm16/include"),
			Source("contrib/libs/llvm16/include"),
		},
	})

	d := includeDirective{kind: includeQuoted, target: internString("llvm/Frontend/OpenMP/OMP.inc")}
	got := sc.resolveSearchPath(Intern("$(S)/contrib/libs/llvm16/lib/Frontend/OpenMP/OMP.cpp"), d)
	want := []VFS{Build("contrib/libs/llvm16/include/llvm/Frontend/OpenMP/OMP.inc")}

	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got=%v, want=%v (Build addincl declared first must win over Source stub)", got, want)
	}
}

// TestScanner_AddInclSourceBeforeBuildKeepsSource is the symmetric anchor:
// when Source is declared before Build, even if a Build registration exists
// for the same target, Source must win (first-match-in-declaration-order).
func TestScanner_AddInclSourceBeforeBuildKeepsSource(t *testing.T) {
	reg := NewCodegenRegistry()
	reg.Register(&GeneratedFileInfo{
		ProducerKvP: "PR",
		OutputPath:  Build("contrib/libs/llvm16/include/llvm/Frontend/OpenMP/OMP.inc"),
	})

	fs := newMemFS(map[string]string{
		"contrib/libs/llvm16/include/llvm/Frontend/OpenMP/OMP.inc": "// committed stub\n",
	})
	scanner := newTestScanner(fs, nil)
	attachCodegen(scanner, reg)
	sc := scanner.NewScanCtx(ScanContext{
		PeerAddInclSet: []VFS{
			Source("contrib/libs/llvm16/include"),
			Build("contrib/libs/llvm16/include"),
		},
	})

	d := includeDirective{kind: includeQuoted, target: internString("llvm/Frontend/OpenMP/OMP.inc")}
	got := sc.resolveSearchPath(Intern("$(S)/contrib/libs/llvm16/lib/Frontend/OpenMP/OMP.cpp"), d)
	want := []VFS{Source("contrib/libs/llvm16/include/llvm/Frontend/OpenMP/OMP.inc")}

	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got=%v, want=%v (Source addincl declared first must win)", got, want)
	}
}

// TestScanner_AddInclBuildOnlyMatchesCodegen locks the OMP.h.inc-style case:
// target lives only in codegen registry (no source stub). With Build addincl
// declared first, resolution must return the Build/generated path. This case
// already worked via the Build fallback loop; the unified ranker must
// preserve it.
func TestScanner_AddInclBuildOnlyMatchesCodegen(t *testing.T) {
	reg := NewCodegenRegistry()
	reg.Register(&GeneratedFileInfo{
		ProducerKvP: "PR",
		OutputPath:  Build("contrib/libs/llvm16/include/llvm/Frontend/OpenMP/OMP.h.inc"),
	})

	scanner := newTestScanner(newMemFS(nil), nil)
	attachCodegen(scanner, reg)
	sc := scanner.NewScanCtx(ScanContext{
		PeerAddInclSet: []VFS{
			Build("contrib/libs/llvm16/include"),
			Source("contrib/libs/llvm16/include"),
		},
	})

	d := includeDirective{kind: includeQuoted, target: internString("llvm/Frontend/OpenMP/OMP.h.inc")}
	got := sc.resolveSearchPath(Intern("$(S)/contrib/libs/llvm16/lib/Frontend/OpenMP/OMP.cpp"), d)
	want := []VFS{Build("contrib/libs/llvm16/include/llvm/Frontend/OpenMP/OMP.h.inc")}

	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got=%v, want=%v (build-only target must resolve via codegen)", got, want)
	}
}
