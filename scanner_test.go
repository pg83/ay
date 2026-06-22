package main

import (
	"bytes"
	"strings"
	"testing"
)

// scanClosure returns srcRel's transitive include closure with the root file
// stripped (element 0).
func scanClosure(scanner *IncludeScanner, srcRel string, cfg ScanContext) []VFS {
	return scanner.newScanCtx(cfg, includeDirectiveParsers.registeredParserFor(srcRel)).closureOf(source(srcRel))[1:]
}

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
	sc := scanner.newScanCtx(newScanContext(scanner.parsers, VFSesFromStrings([]string{"include"}), nil, nil, ""), nil)
	d := IncludeDirective{kind: includeSystem, target: internStr("foo.h")}

	got1 := sc.resolveSearchPath(intern("$(S)/pkg/a.cpp"), dirKey("pkg"), d)
	got2 := sc.resolveSearchPath(intern("$(S)/pkg/b.cpp"), dirKey("pkg"), d)
	want := []VFS{intern("$(S)/include/foo.h")}

	if len(got1) != len(want) || got1[0] != want[0] {
		t.Fatalf("first resolve = %v, want %v", got1, want)
	}

	if len(got2) != len(want) || got2[0] != want[0] {
		t.Fatalf("second resolve = %v, want %v", got2, want)
	}

	if scanner.searchTierFlat.len() != 1 {
		t.Fatalf("searchTierFlat entries = %d, want 1 (shared by target)", scanner.searchTierFlat.len())
	}

	if scanner.searchTierMisses != 1 || scanner.searchTierHits != 1 {
		t.Fatalf("searchTier hits/misses = %d/%d, want 1/1", scanner.searchTierHits, scanner.searchTierMisses)
	}
}

func TestScanner_SearchTierCacheReuse_NotFound(t *testing.T) {
	scanner := newTestScanner(newMemFS(nil), nil)
	sc := scanner.newScanCtx(newScanContext(scanner.parsers, VFSesFromStrings([]string{"include"}), nil, nil, ""), nil)
	d := IncludeDirective{kind: includeSystem, target: internStr("missing.h")}

	got1 := sc.resolveSearchPath(intern("$(S)/pkg/a.cpp"), dirKey("pkg"), d)
	got2 := sc.resolveSearchPath(intern("$(S)/pkg/b.cpp"), dirKey("pkg"), d)

	if got1 != nil || got2 != nil {
		t.Fatalf("missing header resolved unexpectedly: first=%v second=%v", got1, got2)
	}

	if scanner.searchTierFlat.len() != 1 {
		t.Fatalf("searchTierFlat entries = %d, want 1", scanner.searchTierFlat.len())
	}

	if e := scanner.searchTierFlat.get(splitMix64(sc.ctxNum, uint32(internStr("missing.h")))); e != nil && e.found {
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
	sc := scanner.newScanCtx(newScanContext(scanner.parsers, VFSesFromStrings([]string{"include"}), nil, nil, ""), nil)
	got := sc.resolveSearchPath(intern("$(S)/pkg/a.cpp"), dirKey("pkg"), IncludeDirective{kind: includeQuoted, target: internStr("foo.h")})
	want := []VFS{intern("$(S)/pkg/foo.h")}

	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("quoted same-dir resolve = %v, want %v", got, want)
	}

	if scanner.searchTierFlat.len() != 0 {
		t.Fatalf("searchTierFlat entries = %d, want 0 when same-dir quoted wins", scanner.searchTierFlat.len())
	}

	if scanner.searchTierHits != 0 || scanner.searchTierMisses != 0 {
		t.Fatalf("searchTier hits/misses = %d/%d, want 0/0", scanner.searchTierHits, scanner.searchTierMisses)
	}
}

func TestScanner_QuotedSysinclGated_LocalResolved(t *testing.T) {
	fs := newMemFS(map[string]string{
		"yasm/source.cpp":      "#include \"elf.h\"\n",
		"yasm/elf.h":           "// local elf.h\n",
		"foolib/include/elf.h": "// foolib elf.h\n",
	})

	sysincl := SysInclSet{
		{
			Filter:      nil,
			KeyBySource: false,
			pairs: []SysinclPair{
				{key: internStr("elf.h"), paths: []VFS{source("foolib/include/elf.h")}},
			},
		},
	}

	scanner := newTestScanner(fs, sysincl)
	closure := scanClosure(scanner, "yasm/source.cpp", newScanContext(scanner.parsers, nil, nil, nil, ""))

	hasLocal := false
	hasFoo := false

	for _, p := range closure {
		switch {
		case strings.HasSuffix(p.string(), "/yasm/elf.h"):
			hasLocal = true
		case strings.HasSuffix(p.string(), "/foolib/include/elf.h"):
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
		"src/header.h":               "#include \"cxxabi.h\"\n",
		"src/source.cpp":             "#include \"header.h\"\n",
		"libcxxabi/include/cxxabi.h": "// libcxxabi cxxabi.h\n",
		"libcxxrt/include/cxxabi.h":  "// libcxxrt cxxabi.h\n",
	})

	sysincl := SysInclSet{
		{
			Filter:         nil,
			KeyBySource:    false,
			HasMultiTarget: true,
			pairs: []SysinclPair{
				{key: internStr("cxxabi.h"), paths: []VFS{
					source("libcxxabi/include/cxxabi.h"),
					source("libcxxrt/include/cxxabi.h"),
				}},
			},
		},
	}

	scanner := newTestScanner(fs, sysincl)
	closure := scanClosure(scanner, "src/source.cpp", newScanContext(scanner.parsers, VFSesFromStrings([]string{"libcxxabi/include"}), nil, nil, ""))

	hasLibcxxabi := false
	hasLibcxxrt := false

	for _, p := range closure {
		switch {
		case strings.HasSuffix(p.string(), "/libcxxabi/include/cxxabi.h"):
			hasLibcxxabi = true
		case strings.HasSuffix(p.string(), "/libcxxrt/include/cxxabi.h"):
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
		"libcxxrt/dwarf_eh.h":     "#include \"unwind.h\"\n",
		"libcxxrt/source.cc":      "#include \"dwarf_eh.h\"\n",
		"libcxxrt/unwind.h":       "// libcxxrt unwind.h\n",
		"libcxx/include/unwind.h": "// libcxx unwind.h\n",
	})

	sysincl := SysInclSet{
		{
			Filter:         nil,
			KeyBySource:    false,
			HasMultiTarget: true,
			pairs: []SysinclPair{
				{key: internStr("unwind.h"), paths: []VFS{
					source("libcxx/include/unwind.h"),
					source("libcxxrt/unwind.h"),
				}},
			},
		},
	}

	scanner := newTestScanner(fs, sysincl)
	closure := scanClosure(scanner, "libcxxrt/source.cc", newScanContext(scanner.parsers, VFSesFromStrings([]string{"libcxxrt"}), nil, nil, ""))

	hasLibcxxrt := false
	hasLibcxxSpurious := false

	for _, p := range closure {
		switch {
		case strings.HasSuffix(p.string(), "/libcxxrt/unwind.h"):
			hasLibcxxrt = true
		case strings.HasSuffix(p.string(), "/libcxx/include/unwind.h"):
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
		"src/source.cpp":          "#include \"absent.h\"\n",
		"foolib/include/absent.h": "// foolib absent.h\n",
	})

	sysincl := SysInclSet{
		{
			Filter:      nil,
			KeyBySource: false,
			pairs: []SysinclPair{
				{key: internStr("absent.h"), paths: []VFS{source("foolib/include/absent.h")}},
			},
		},
	}

	scanner := newTestScanner(fs, sysincl)
	closure := scanClosure(scanner, "src/source.cpp", newScanContext(scanner.parsers, nil, nil, nil, ""))

	hasFoolib := false

	for _, p := range closure {
		if strings.HasSuffix(p.string(), "/foolib/include/absent.h") {
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
		"libcxxrt/source.cpp":        "#include <unwind.h>\n",
		"libcxxrt/unwind.h":          "// libcxxrt unwind.h\n",
		"libunwind/include/unwind.h": "// libunwind unwind.h\n",
	})

	sysincl := SysInclSet{
		{
			Filter:      nil,
			KeyBySource: false,
			pairs: []SysinclPair{
				{key: internStr("unwind.h"), paths: []VFS{source("libunwind/include/unwind.h")}},
			},
		},
	}

	scanner := newTestScanner(fs, sysincl)
	closure := scanClosure(scanner, "libcxxrt/source.cpp", newScanContext(scanner.parsers, VFSesFromStrings([]string{"libcxxrt"}), nil, nil, ""))

	hasLocal := false
	hasLibunwind := false

	for _, p := range closure {
		switch {
		case strings.HasSuffix(p.string(), "/libcxxrt/unwind.h"):
			hasLocal = true
		case strings.HasSuffix(p.string(), "/libunwind/include/unwind.h"):
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

	dblock := make([]IncludeDirective, 64)
	dirs := dblock[:parseYasmIncludes(in, dblock, 0)]

	if len(dirs) != 1 {
		t.Fatalf("got %d directives, want 1; %+v", len(dirs), dirs)
	}

	if dirs[0].target.string() != "defs.asm" {
		t.Errorf("target = %q, want %q", dirs[0].target.string(), "defs.asm")
	}

	if dirs[0].kind != includeQuoted {
		t.Errorf("kind = %v, want includeQuoted", dirs[0].kind)
	}
}

func TestParseYasmIncludes_UppercaseDirective(t *testing.T) {
	in := []byte(`%INCLUDE "randomah.asi"
`)

	dblock := make([]IncludeDirective, 64)
	dirs := dblock[:parseYasmIncludes(in, dblock, 0)]

	if len(dirs) != 1 {
		t.Fatalf("got %d directives, want 1; %+v", len(dirs), dirs)
	}

	if dirs[0].target.string() != "randomah.asi" {
		t.Errorf("target = %q, want %q", dirs[0].target.string(), "randomah.asi")
	}

	if dirs[0].kind != includeQuoted {
		t.Errorf("kind = %v, want includeQuoted", dirs[0].kind)
	}
}

func TestParseYasmIncludes_LineCommentIgnored(t *testing.T) {
	in := []byte(`; %include "ghost.asm"
%include "real.asm"
`)

	dblock := make([]IncludeDirective, 64)
	dirs := dblock[:parseYasmIncludes(in, dblock, 0)]

	if len(dirs) != 1 {
		t.Fatalf("got %d directives, want 1; %+v", len(dirs), dirs)
	}

	if dirs[0].target.string() != "real.asm" {
		t.Errorf("target = %q, want %q", dirs[0].target.string(), "real.asm")
	}
}

func TestParseYasmIncludes_TrailingSemicolonComment(t *testing.T) {
	in := []byte(`%include "instrset64.asm"              ; include code for InstructionSet function
`)

	dblock := make([]IncludeDirective, 64)
	dirs := dblock[:parseYasmIncludes(in, dblock, 0)]

	if len(dirs) != 1 {
		t.Fatalf("got %d directives, want 1; %+v", len(dirs), dirs)
	}

	if dirs[0].target.string() != "instrset64.asm" {
		t.Errorf("target = %q, want %q", dirs[0].target.string(), "instrset64.asm")
	}
}

func TestParseYasmIncludes_NoMatchOnCInclude(t *testing.T) {
	in := []byte(`#include "foo.h"
`)

	dblock := make([]IncludeDirective, 64)
	dirs := dblock[:parseYasmIncludes(in, dblock, 0)]

	if len(dirs) != 0 {
		t.Errorf("got %d directives, want 0; %+v", len(dirs), dirs)
	}
}

func TestParseYasmIncludes_AngleBracketForm(t *testing.T) {
	in := []byte(`%include <sysmacros.asi>
`)

	dblock := make([]IncludeDirective, 64)
	dirs := dblock[:parseYasmIncludes(in, dblock, 0)]

	if len(dirs) != 1 {
		t.Fatalf("got %d directives, want 1; %+v", len(dirs), dirs)
	}

	if dirs[0].target.string() != "sysmacros.asi" {
		t.Errorf("target = %q, want %q", dirs[0].target.string(), "sysmacros.asi")
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
	dirs := scanner.sourceParsedBuckets(intern("$(S)/src.proto")).bucket(parsedIncludesLocal)

	if len(dirs) != 3 {
		t.Fatalf("got %d directives, want 3; %+v", len(dirs), dirs)
	}

	got := []string{dirs[0].target.string(), dirs[1].target.string(), dirs[2].target.string()}
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
	parsed := scanner.parsedIncludes(intern("$(S)/src.proto"))
	local := parsed.bucket(parsedIncludesLocal)
	hcpp := parsed.bucket(parsedIncludesHeader)

	if len(local) != 4 {
		t.Fatalf("got %d local entries, want 4; %+v", len(local), local)
	}
	if len(hcpp) != 4 {
		t.Fatalf("got %d h+cpp entries, want 4; %+v", len(hcpp), hcpp)
	}

	wantHCPP := []string{"a.pb.h", "b.pb.h", "c.pb.h", "d.ev.pb.h"}
	for i, want := range wantHCPP {
		if hcpp[i].target.string() != want {
			t.Fatalf("hcpp[%d].target.String() = %q, want %q; all=%+v", i, hcpp[i].target.string(), want, hcpp)
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
	parsed := scanner.parsedIncludes(intern("$(S)/src.rl6"))
	local := parsed.bucket(parsedIncludesLocal)
	native := parsed.bucket(parsedIncludesRagelNative)
	hcpp := parsed.bucket(parsedIncludesHeader)

	// C includes only (ragel-native goes to its own bucket)
	if len(local) != 2 {
		t.Fatalf("got %d local entries, want 2 (C includes only); %+v", len(local), local)
	}
	wantLocalTargets := []string{"outer.h", "tail.h"}
	wantLocalKinds := []IncludeKind{includeSystem, includeQuoted}
	for i := range wantLocalTargets {
		if local[i].target.string() != wantLocalTargets[i] {
			t.Fatalf("local[%d].target.String() = %q, want %q; all=%+v", i, local[i].target.string(), wantLocalTargets[i], local)
		}
		if local[i].kind != wantLocalKinds[i] {
			t.Fatalf("local[%d].kind = %v, want %v", i, local[i].kind, wantLocalKinds[i])
		}
	}

	// Ragel-native includes (deduped) in their own bucket
	if len(native) != 2 {
		t.Fatalf("got %d ragel-native entries, want 2; %+v", len(native), native)
	}
	wantNativeTargets := []string{"machine.rl", "machine2.rl"}
	for i := range wantNativeTargets {
		if native[i].target.string() != wantNativeTargets[i] {
			t.Fatalf("native[%d].target.String() = %q, want %q; all=%+v", i, native[i].target.string(), wantNativeTargets[i], native)
		}
		if native[i].kind != includeQuoted {
			t.Fatalf("native[%d].kind = %v, want includeQuoted", i, native[i].kind)
		}
	}

	// h+cpp (generated cpp's induced set): self-include leads, C/C++ directives follow.
	wantHCPP := []struct {
		target string
		kind   IncludeKind
	}{
		{"src.rl6", includeQuoted},
		{"outer.h", includeSystem},
		{"tail.h", includeQuoted},
	}

	if len(hcpp) != len(wantHCPP) {
		t.Fatalf("got %d h+cpp entries, want %d; %+v", len(hcpp), len(wantHCPP), hcpp)
	}

	for i, want := range wantHCPP {
		if hcpp[i].target.string() != want.target || hcpp[i].kind != want.kind {
			t.Fatalf("hcpp[%d] = %+v, want {%s %v}", i, hcpp[i], want.target, want.kind)
		}
	}
}

// TestScanner_RagelNativeInclude_DoesNotBleedCHeaders verifies the ragel walk
// relation: a ragel file's closure follows its native %include edges ONLY; no C
// headers join. The C side rides as the induced h+cpp set on the generated cpp.
func TestScanner_RagelNativeInclude_DoesNotBleedCHeaders(t *testing.T) {
	sysincl := parseSysInclYAML("test.yml", []byte(`
- includes:
  - vector: stl/vector
  - numeric: stl/numeric
`), func(Warn) {})

	fs := newMemFS(map[string]string{
		"pkg/main.rl6": `#include <vector>
%%{
include "sub.rl6";
}%%
`,
		"pkg/sub.rl6": `#include <numeric>
%%{
machine Sub;
}%%
`,
		"stl/vector":  "// vector\n",
		"stl/numeric": "// numeric\n",
	})

	scanner := newTestScanner(fs, sysincl)

	sc := scanner.newScanCtx(newScanContext(scanner.parsers, VFSesFromStrings([]string{"stl"}), nil, nil, ""), nil)

	closure := sc.closureOf(intern("$(S)/pkg/main.rl6"))

	closureSet := make(map[string]bool, len(closure))
	for _, v := range closure {
		closureSet[v.string()] = true
	}

	// main.rl6 itself
	if !closureSet["$(S)/pkg/main.rl6"] {
		t.Errorf("closure missing $(S)/pkg/main.rl6: %v", closure)
	}

	// sub.rl6 is a ragel-native edge — walked directly.
	if !closureSet["$(S)/pkg/sub.rl6"] {
		t.Errorf("closure missing $(S)/pkg/sub.rl6 (ragel-native edge): %v", closure)
	}

	// vector (C include of main.rl6) is NOT a walkable edge — it reaches the
	// compile via the induced h+cpp set on the generated cpp.
	if closureSet["$(S)/stl/vector"] {
		t.Errorf("closure should NOT contain $(S)/stl/vector (C side rides as induced h+cpp): %v", closure)
	}

	// numeric (C include of sub.rl6) must NOT bleed in
	if closureSet["$(S)/stl/numeric"] {
		t.Errorf("closure should NOT contain $(S)/stl/numeric (C header of ragel-native-included sub.rl6 must not bleed): %v", closure)
	}

	// The generated cpp's induced set: self-include + main's C directives.
	induced := scanner.parsers.sourceParsedBuckets(intern("$(S)/pkg/main.rl6"), nil).bucket(parsedIncludesCpp)
	if len(induced) != 2 || induced[0].target.string() != "pkg/main.rl6" || induced[1].target.string() != "vector" {
		t.Errorf("induced h+cpp of main.rl6 = %v, want [{pkg/main.rl6} {vector}]", induced)
	}
}

func TestParsedIncludes_LeadingUTF8BOM(t *testing.T) {
	scanner := newTestScanner(newMemFS(map[string]string{
		"bom.cpp": "\xef\xbb\xbf#include \"sibling.h\"\n#include <system.h>\n",
	}), SysInclSet{})
	local := scanner.parsedIncludes(intern("$(S)/bom.cpp")).bucket(parsedIncludesLocal)

	if len(local) != 2 {
		t.Fatalf("got %d local entries, want 2 (BOM not stripped?); %+v", len(local), local)
	}
	if local[0].target.string() != "sibling.h" || local[0].kind != includeQuoted {
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
	parsed := scanner.parsedIncludes(intern("$(S)/src.swg"))
	local := parsed.bucket(parsedIncludesLocal)
	hcpp := parsed.bucket(parsedIncludesHeader)

	// A root .swg outside the swig library leads with the 5 implicit
	// language-runtime system includes (the parser's own directives).
	if len(local) != 8 {
		t.Fatalf("got %d local entries, want 8 (5 implicit + 3 parsed); %+v", len(local), local)
	}

	wantLocalTargets := []string{
		"swig.swg", "go.swg", "java.swg", "perl5.swg", "python.swg",
		"a.i", "b.i", "c.h",
	}
	wantLocalKinds := []IncludeKind{
		includeSystem, includeSystem, includeSystem, includeSystem, includeSystem,
		includeQuoted, includeSystem, includeQuoted,
	}
	for i := range wantLocalTargets {
		if local[i].target.string() != wantLocalTargets[i] {
			t.Fatalf("local[%d].target.String() = %q, want %q; all=%+v", i, local[i].target.string(), wantLocalTargets[i], local)
		}
		if local[i].kind != wantLocalKinds[i] {
			t.Fatalf("local[%d].kind = %v, want %v", i, local[i].kind, wantLocalKinds[i])
		}
	}

	if len(hcpp) != 1 {
		t.Fatalf("got %d h+cpp entries, want 1; %+v", len(hcpp), hcpp)
	}
	if hcpp[0].target.string() != "block.h" || hcpp[0].kind != includeQuoted {
		t.Fatalf("h+cpp = %+v, want block.h quoted", hcpp)
	}
}

func TestScanDirectives_DispatchByExtension(t *testing.T) {
	scanner := newTestScanner(newMemFS(map[string]string{
		"src.asm": "%include \"defs.asm\"\n#include \"should-not-match.h\"\n",
		"src.h":   "#include \"real.h\"\n%include \"should-not-match.asm\"\n",
	}), SysInclSet{})

	asmDirs := scanner.scanDirectives(intern("$(S)/src.asm"))
	hDirs := scanner.scanDirectives(intern("$(S)/src.h"))

	if len(asmDirs) != 1 || asmDirs[0].target.string() != "defs.asm" {
		t.Errorf("asm dispatch failed: got %+v, want one directive targeting defs.asm", asmDirs)
	}

	if len(hDirs) != 1 || hDirs[0].target.string() != "real.h" {
		t.Errorf("h dispatch failed: got %+v, want one directive targeting real.h", hDirs)
	}
}

func TestScanDirectives_AsiDispatchesToYasm(t *testing.T) {
	scanner := newTestScanner(newMemFS(map[string]string{
		"src.asi": "%include \"nested.asi\"\n",
	}), SysInclSet{})

	dirs := scanner.scanDirectives(intern("$(S)/src.asi"))

	if len(dirs) != 1 || dirs[0].target.string() != "nested.asi" {
		t.Errorf(".asi dispatch failed: got %+v, want one directive targeting nested.asi", dirs)
	}
}

func TestScanDirectives_G4UsesEmptyParser(t *testing.T) {
	scanner := newTestScanner(newMemFS(map[string]string{
		"src.g4": "#include \"ghost.h\"\ngrammar X;\n",
	}), SysInclSet{})
	dirs := scanner.scanDirectives(intern("$(S)/src.g4"))

	if len(dirs) != 0 {
		t.Errorf(".g4 should use empty parser, got %+v", dirs)
	}
}

func TestScanDirectives_InSuffixUsesUnderlyingExtension(t *testing.T) {
	scanner := newTestScanner(newMemFS(map[string]string{
		"src.g4.in": "#include \"ghost.h\"\ngrammar X;\n",
	}), SysInclSet{})
	dirs := scanner.scanDirectives(intern("$(S)/src.g4.in"))

	if len(dirs) != 0 {
		t.Errorf(".g4.in should inherit .g4 empty parser, got %+v", dirs)
	}
}

func TestScanDirectives_MacroIndirectAugmentation(t *testing.T) {
	const rel = "contrib/libs/openssl/crypto/uid.c"

	scanner := newTestScanner(newMemFS(map[string]string{
		rel: "#include <openssl/crypto.h>\n# include OPENSSL_UNISTD\n",
	}), SysInclSet{})
	dirs := scanner.scanDirectives(source(rel))

	var hasCrypto, hasUnistd bool

	for _, d := range dirs {
		if d.target.string() == "openssl/crypto.h" && d.kind == includeSystem {
			hasCrypto = true
		}

		if d.target.string() == "unistd.h" && d.kind == includeSystem {
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

func (s *IncludeScanner) scanDirectives(vfsPath VFS) []IncludeDirective {
	return s.parsers.parsedIncludes(vfsPath, nil)
}

func (s *IncludeScanner) parsedIncludes(vfsPath VFS) ParsedIncludeSet {
	return s.parsers.sourceParsedBuckets(vfsPath, nil)
}

func (s *IncludeScanner) sourceParsedBuckets(vfsPath VFS) ParsedIncludeSet {
	return s.parsers.sourceParsedBuckets(vfsPath, nil)
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
	closure := scanClosure(scanner, "pkg/mod.pyx", newScanContext(scanner.parsers, []VFS{
		intern("$(S)/"),
		intern("$(S)/contrib/tools/cython/Cython/Includes"),
	}, nil, nil, ""))

	assertHasVFS(t, closure, intern("$(S)/util/generic/string.pxd"))
	assertHasVFS(t, closure, intern("$(S)/contrib/tools/cython_py2/Cython/Includes/libcpp/string.pxd"))
	assertHasVFS(t, closure, intern("$(S)/contrib/tools/cython_py2/Cython/Includes/libc/string.pxd"))
	assertLacksVFS(t, closure, intern("$(S)/contrib/tools/cython/Cython/Includes/libcpp/string.pxd"))
	assertLacksVFS(t, closure, intern("$(S)/contrib/tools/cython/Cython/Includes/libc/string.pxd"))
	assertLacksVFS(t, closure, intern("$(S)/contrib/tools/cython/Cython/Includes/libcpp/string_view.pxd"))
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
	closure := scanClosure(scanner, "pkg/mod.pyx", newScanContext(scanner.parsers, []VFS{
		intern("$(S)/"),
		intern("$(S)/contrib/tools/cython/Cython/Includes"),
	}, nil, nil, ""))

	assertHasVFS(t, closure, intern("$(S)/contrib/tools/cython/Cython/Includes/libcpp/pair.pxd"))
	assertHasVFS(t, closure, intern("$(S)/contrib/tools/cython/Cython/Includes/libcpp/utility.pxd"))
	assertHasVFS(t, closure, intern("$(S)/contrib/tools/cython_py2/Cython/Includes/libcpp/pair.pxd"))
	assertHasVFS(t, closure, intern("$(S)/contrib/tools/cython_py2/Cython/Includes/libcpp/utility.pxd"))
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
	closure := scanClosure(scanner, "pkg/mod.pyx", newScanContext(scanner.parsers, []VFS{
		intern("$(S)/"),
		intern("$(S)/contrib/tools/cython/Cython/Includes"),
	}, nil, nil, ""))

	assertHasVFS(t, closure, intern("$(S)/contrib/tools/cython/Cython/Includes/libcpp/__init__.pxd"))
	assertLacksVFS(t, closure, intern("$(S)/contrib/tools/cython_py2/Cython/Includes/libcpp/__init__.pxd"))
	assertHasVFS(t, closure, intern("$(S)/contrib/tools/cython/Cython/Includes/libc/stdint.pxd"))
	assertHasVFS(t, closure, intern("$(S)/contrib/tools/cython_py2/Cython/Includes/libc/stdint.pxd"))
}

// attachCodegen wires a CodegenRegistry onto an existing scanner (the codegen
// field), the way the real gen pipeline does.
func attachCodegen(scanner *IncludeScanner, reg *CodegenRegistry) {
	scanner.codegen = reg
}

// TestScanner_AddInclBuildBeforeSourceWinsWhenBothExist: when ADDINCL declares a
// Build-rooted prefix BEFORE a Source-rooted one and the target exists under both
// (source stub + codegen output), the Build path wins (declaration-order
// first-match).
func TestScanner_AddInclBuildBeforeSourceWinsWhenBothExist(t *testing.T) {
	reg := newCodegenRegistry()
	reg.register(&GeneratedFileInfo{
		ProducerKvP: pkPR,
		OutputPath:  build("contrib/libs/llvm16/include/llvm/Frontend/OpenMP/OMP.inc"),
	})

	fs := newMemFS(map[string]string{
		"contrib/libs/llvm16/include/llvm/Frontend/OpenMP/OMP.inc": "// committed stub\n",
	})
	scanner := newTestScanner(fs, nil)
	attachCodegen(scanner, reg)
	sc := scanner.newScanCtx(newScanContext(scanner.parsers, nil, []VFS{
		build("contrib/libs/llvm16/include"),
		source("contrib/libs/llvm16/include"),
	}, nil, ""), nil)

	d := IncludeDirective{kind: includeQuoted, target: internStr("llvm/Frontend/OpenMP/OMP.inc")}
	got := sc.resolveSearchPath(intern("$(S)/contrib/libs/llvm16/lib/Frontend/OpenMP/OMP.cpp"), dirKey("contrib/libs/llvm16/lib/Frontend/OpenMP"), d)
	want := []VFS{build("contrib/libs/llvm16/include/llvm/Frontend/OpenMP/OMP.inc")}

	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got=%v, want=%v (Build addincl declared first must win over Source stub)", got, want)
	}
}

// TestScanner_AddInclSourceBeforeBuildKeepsSource is the symmetric anchor: Source
// declared first must win even if a Build registration exists.
func TestScanner_AddInclSourceBeforeBuildKeepsSource(t *testing.T) {
	reg := newCodegenRegistry()
	reg.register(&GeneratedFileInfo{
		ProducerKvP: pkPR,
		OutputPath:  build("contrib/libs/llvm16/include/llvm/Frontend/OpenMP/OMP.inc"),
	})

	fs := newMemFS(map[string]string{
		"contrib/libs/llvm16/include/llvm/Frontend/OpenMP/OMP.inc": "// committed stub\n",
	})
	scanner := newTestScanner(fs, nil)
	attachCodegen(scanner, reg)
	sc := scanner.newScanCtx(newScanContext(scanner.parsers, nil, []VFS{
		source("contrib/libs/llvm16/include"),
		build("contrib/libs/llvm16/include"),
	}, nil, ""), nil)

	d := IncludeDirective{kind: includeQuoted, target: internStr("llvm/Frontend/OpenMP/OMP.inc")}
	got := sc.resolveSearchPath(intern("$(S)/contrib/libs/llvm16/lib/Frontend/OpenMP/OMP.cpp"), dirKey("contrib/libs/llvm16/lib/Frontend/OpenMP"), d)
	want := []VFS{source("contrib/libs/llvm16/include/llvm/Frontend/OpenMP/OMP.inc")}

	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got=%v, want=%v (Source addincl declared first must win)", got, want)
	}
}

// TestScanner_AddInclBuildOnlyMatchesCodegen: target lives only in the codegen
// registry (no source stub). With Build addincl declared first, resolution must
// return the Build/generated path.
func TestScanner_AddInclBuildOnlyMatchesCodegen(t *testing.T) {
	reg := newCodegenRegistry()
	reg.register(&GeneratedFileInfo{
		ProducerKvP: pkPR,
		OutputPath:  build("contrib/libs/llvm16/include/llvm/Frontend/OpenMP/OMP.h.inc"),
	})

	scanner := newTestScanner(newMemFS(nil), nil)
	attachCodegen(scanner, reg)
	sc := scanner.newScanCtx(newScanContext(scanner.parsers, nil, []VFS{
		build("contrib/libs/llvm16/include"),
		source("contrib/libs/llvm16/include"),
	}, nil, ""), nil)

	d := IncludeDirective{kind: includeQuoted, target: internStr("llvm/Frontend/OpenMP/OMP.h.inc")}
	got := sc.resolveSearchPath(intern("$(S)/contrib/libs/llvm16/lib/Frontend/OpenMP/OMP.cpp"), dirKey("contrib/libs/llvm16/lib/Frontend/OpenMP"), d)
	want := []VFS{build("contrib/libs/llvm16/include/llvm/Frontend/OpenMP/OMP.h.inc")}

	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got=%v, want=%v (build-only target must resolve via codegen)", got, want)
	}
}

func TestScanCtx_Resolve_RootedTargetBindsDirectly(t *testing.T) {
	// A rooted directive target binds to its root without include search, sysincl,
	// or FS checks. The memFS is empty on purpose: nothing else could find these.
	scanner := newTestScanner(newMemFS(nil), nil)
	sc := scanner.newScanCtx(newScanContext(scanner.parsers, nil, nil, nil, ""), nil)

	for _, target := range []string{"$(S)/util/generic/typetraits.h", "$(B)/pkg/gen.h"} {
		d := IncludeDirective{kind: includeQuoted, target: internStr(target)}
		got := sc.resolve(intern("$(S)/pkg/a.cpp"), dirKey("pkg"), d)

		if len(got) != 1 || got[0] != intern(target) {
			t.Errorf("resolve(%s) = %v, want [%s]", target, got, target)
		}
	}
}
