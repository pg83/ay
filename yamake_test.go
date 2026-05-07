package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// archiverYaMake is the verbatim content of tools/archiver/ya.make from the
// upstream ya checkout, inlined so the test does not depend on that path
// existing on disk. See TestParseArchiverYaMakeOnDisk for the optional
// disk-backed smoke test that pins this inlined copy against the real file
// when available.
const archiverYaMake = `PROGRAM()

PEERDIR(
    library/cpp/archive
    library/cpp/digest/md5
    library/cpp/getopt/small
)

SRCS(
    main.cpp
)

SET(IDE_FOLDER "_Builders")

END()
`

// libraryArchiveYaMake mirrors library/cpp/archive/ya.make from the
// upstream ya checkout, inlined for the same reason as archiverYaMake.
const libraryArchiveYaMake = `LIBRARY()

SRCS(
    yarchive.cpp
    yarchive.h
    directory_models_archive_reader.cpp
    directory_models_archive_reader.h
    models_archive_reader.cpp
)

END()

RECURSE_FOR_TESTS(
    ut
)
`

func TestParseArchiverYaMake(t *testing.T) {
	mf, err := Parse("tools/archiver/ya.make", []byte(archiverYaMake))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if got, want := len(mf.Stmts), 5; got != want {
		t.Fatalf("len(Stmts) = %d, want %d (stmts=%#v)", got, want, mf.Stmts)
	}

	// 1: PROGRAM()
	if m, ok := mf.Stmts[0].(*ModuleStmt); !ok {
		t.Fatalf("Stmts[0] = %T, want *ModuleStmt", mf.Stmts[0])
	} else {
		if m.Name != "PROGRAM" {
			t.Errorf("Stmts[0].Name = %q, want %q", m.Name, "PROGRAM")
		}
		if len(m.Args) != 0 {
			t.Errorf("Stmts[0].Args = %v, want empty", m.Args)
		}
		if m.Line == 0 {
			t.Errorf("Stmts[0].Line = 0, want non-zero")
		}
	}

	// 2: PEERDIR(library/cpp/archive library/cpp/digest/md5 library/cpp/getopt/small)
	if p, ok := mf.Stmts[1].(*PeerdirStmt); !ok {
		t.Fatalf("Stmts[1] = %T, want *PeerdirStmt", mf.Stmts[1])
	} else {
		want := []string{"library/cpp/archive", "library/cpp/digest/md5", "library/cpp/getopt/small"}
		if !equalStrings(p.Paths, want) {
			t.Errorf("PEERDIR.Paths = %v, want %v", p.Paths, want)
		}
	}

	// 3: SRCS(main.cpp)
	if s, ok := mf.Stmts[2].(*SrcsStmt); !ok {
		t.Fatalf("Stmts[2] = %T, want *SrcsStmt", mf.Stmts[2])
	} else {
		want := []string{"main.cpp"}
		if !equalStrings(s.Sources, want) {
			t.Errorf("SRCS.Sources = %v, want %v", s.Sources, want)
		}
	}

	// 4: SET(IDE_FOLDER "_Builders")
	if s, ok := mf.Stmts[3].(*SetStmt); !ok {
		t.Fatalf("Stmts[3] = %T, want *SetStmt", mf.Stmts[3])
	} else {
		if s.Name != "IDE_FOLDER" {
			t.Errorf("SET.Name = %q, want %q", s.Name, "IDE_FOLDER")
		}
		if s.Value != "_Builders" {
			t.Errorf("SET.Value = %q, want %q", s.Value, "_Builders")
		}
	}

	// 5: END()
	if _, ok := mf.Stmts[4].(*EndStmt); !ok {
		t.Fatalf("Stmts[4] = %T, want *EndStmt", mf.Stmts[4])
	}
}

func TestParseLibraryArchiveYaMake(t *testing.T) {
	mf, err := Parse("library/cpp/archive/ya.make", []byte(libraryArchiveYaMake))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	sawLibrary := false
	sawEnd := false
	for _, s := range mf.Stmts {
		switch v := s.(type) {
		case *ModuleStmt:
			if v.Name == "LIBRARY" {
				sawLibrary = true
			}
		case *EndStmt:
			sawEnd = true
		}
	}
	if !sawLibrary && !sawEnd {
		t.Fatalf("expected at least one of LIBRARY or END to appear; got %d stmts: %#v", len(mf.Stmts), mf.Stmts)
	}
}

// TestParseArchiverYaMakeOnDisk is a smoke test that pins our inlined
// copy of tools/archiver/ya.make against the real file when the upstream
// checkout is available locally. Skipped (not failed) when the file is
// missing, so the suite stays portable.
func TestParseArchiverYaMakeOnDisk(t *testing.T) {
	const path = "/home/pg/monorepo/yatool_orig/tools/archiver/ya.make"
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("requires /home/pg/monorepo/yatool_orig/... checked out at this path")
	}
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v", path, err)
	}
	if string(data) != archiverYaMake {
		t.Fatalf("inlined archiverYaMake drifted from %s; please re-sync the constant", path)
	}
}

func TestUnknownMacro(t *testing.T) {
	src := []byte("FROBNICATE(foo bar)\n")
	mf, err := Parse("test.input", src)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mf.Stmts) != 1 {
		t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
	}
	u, ok := mf.Stmts[0].(*UnknownStmt)
	if !ok {
		t.Fatalf("Stmts[0] = %T, want *UnknownStmt", mf.Stmts[0])
	}
	if u.Name != "FROBNICATE" {
		t.Errorf("UnknownStmt.Name = %q, want %q", u.Name, "FROBNICATE")
	}
	want := []string{"foo", "bar"}
	if !equalStrings(u.Args, want) {
		t.Errorf("UnknownStmt.Args = %v, want %v", u.Args, want)
	}
}

func TestCommentHandling(t *testing.T) {
	src := []byte("# this is a comment\nPROGRAM()\n")
	mf, err := Parse("test.input", src)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mf.Stmts) != 1 {
		t.Fatalf("len(Stmts) = %d, want 1 (comment should be dropped)", len(mf.Stmts))
	}
	m, ok := mf.Stmts[0].(*ModuleStmt)
	if !ok {
		t.Fatalf("Stmts[0] = %T, want *ModuleStmt", mf.Stmts[0])
	}
	if m.Name != "PROGRAM" {
		t.Errorf("ModuleStmt.Name = %q, want %q", m.Name, "PROGRAM")
	}
	// PROGRAM is on line 2; ensure line tracking advanced past the comment.
	if m.Line != 2 {
		t.Errorf("PROGRAM.Line = %d, want 2", m.Line)
	}
}

func TestMultilineMacro(t *testing.T) {
	src := []byte("PEERDIR(\n  a/b\n  c/d\n)\n")
	mf, err := Parse("test.input", src)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mf.Stmts) != 1 {
		t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
	}
	p, ok := mf.Stmts[0].(*PeerdirStmt)
	if !ok {
		t.Fatalf("Stmts[0] = %T, want *PeerdirStmt", mf.Stmts[0])
	}
	want := []string{"a/b", "c/d"}
	if !equalStrings(p.Paths, want) {
		t.Errorf("PEERDIR.Paths = %v, want %v", p.Paths, want)
	}
}

func TestSetQuotedAndUnquoted(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantName string
		wantVal  string
	}{
		{"quoted", `SET(IDE_FOLDER "_Builders")`, "IDE_FOLDER", "_Builders"},
		{"unquoted", `SET(NAME bare_value)`, "NAME", "bare_value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mf, err := Parse("test.input", []byte(tc.src))
			if err != nil {
				t.Fatalf("Parse(%q) failed: %v", tc.src, err)
			}
			if len(mf.Stmts) != 1 {
				t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
			}
			s, ok := mf.Stmts[0].(*SetStmt)
			if !ok {
				t.Fatalf("Stmts[0] = %T, want *SetStmt", mf.Stmts[0])
			}
			if s.Name != tc.wantName {
				t.Errorf("SET.Name = %q, want %q", s.Name, tc.wantName)
			}
			if s.Value != tc.wantVal {
				t.Errorf("SET.Value = %q, want %q", s.Value, tc.wantVal)
			}
		})
	}
}

func TestErrorCases(t *testing.T) {
	cases := []struct {
		name        string
		src         string
		wantSubstr  string // substring expected in the error message
		wantNonZero bool   // line should be > 0
	}{
		{"unterminated string", `"hello`, "unterminated string", true},
		{"unterminated macro", `PROGRAM(`, "unterminated macro", true},
		{"weird character", `@@@`, "unexpected character", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse("test.input", []byte(tc.src))
			if err == nil {
				t.Fatalf("Parse(%q) returned nil error, want *ParseError", tc.src)
			}
			var pe *ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("Parse(%q) returned %T, want *ParseError", tc.src, err)
			}
			if tc.wantNonZero && pe.Line == 0 {
				t.Errorf("ParseError.Line = 0, want non-zero")
			}
			if !strings.Contains(pe.Message, tc.wantSubstr) {
				t.Errorf("ParseError.Message = %q, want substring %q", pe.Message, tc.wantSubstr)
			}
		})
	}
}

func TestSetArityError(t *testing.T) {
	_, err := Parse("test.input", []byte(`SET(only_one_arg)`))
	if err == nil {
		t.Fatalf("Parse returned nil error, want *ParseError")
	}
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("Parse returned %T, want *ParseError", err)
	}
	if pe.Line == 0 {
		t.Errorf("ParseError.Line = 0, want non-zero")
	}
	if !strings.Contains(pe.Message, "SET") {
		t.Errorf("ParseError.Message = %q, want it to mention SET", pe.Message)
	}
}

// TestLineEndings (D02) pins that LF, CRLF, and lone-CR sources all
// produce identical line numbers for tokens. Regression test for the
// earlier bug where '\r' was treated as plain whitespace and never
// bumped the line counter, so CRLF sources reported every token on
// line 1.
func TestLineEndings(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"lf", "# c\nPROGRAM()\n# c2\nEND()\n"},
		{"crlf", "# c\r\nPROGRAM()\r\n# c2\r\nEND()\r\n"},
		{"cr", "# c\rPROGRAM()\r# c2\rEND()\r"},
	}
	var got [3][]int
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mf, err := Parse("test.input", []byte(tc.src))
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
			if len(mf.Stmts) != 2 {
				t.Fatalf("len(Stmts) = %d, want 2", len(mf.Stmts))
			}
			lines := make([]int, 0, 2)
			for _, s := range mf.Stmts {
				switch v := s.(type) {
				case *ModuleStmt:
					lines = append(lines, v.Line)
				case *EndStmt:
					lines = append(lines, v.Line)
				default:
					t.Fatalf("unexpected stmt %T", s)
				}
			}
			got[i] = lines
		})
	}
	if !equalInts(got[0], got[1]) {
		t.Errorf("CRLF lines %v differ from LF lines %v", got[1], got[0])
	}
	if !equalInts(got[0], got[2]) {
		t.Errorf("lone-CR lines %v differ from LF lines %v", got[2], got[0])
	}
	want := []int{2, 4}
	if !equalInts(got[0], want) {
		t.Errorf("LF baseline lines = %v, want %v", got[0], want)
	}
}

// TestStringRejectsNewline (D03) pins that an unterminated string
// containing a literal newline returns a *ParseError pinned at the
// opening quote, not silently consumed across lines.
func TestStringRejectsNewline(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"lf", "SET(N \"no close\nEND()"},
		{"crlf", "SET(N \"no close\r\nEND()"},
		{"cr", "SET(N \"no close\rEND()"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse("test.input", []byte(tc.src))
			if err == nil {
				t.Fatalf("Parse returned nil error, want *ParseError")
			}
			var pe *ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("Parse returned %T, want *ParseError", err)
			}
			if !strings.Contains(pe.Message, "unterminated string") {
				t.Errorf("ParseError.Message = %q, want substring %q", pe.Message, "unterminated string")
			}
			// The error must be pinned at the opening quote on line 1,
			// not at the closing-quote-that-never-arrived on later lines.
			if pe.Line != 1 {
				t.Errorf("ParseError.Line = %d, want 1 (the opening-quote line)", pe.Line)
			}
		})
	}
}

// TestLowercaseAndMixedCaseMacro (D04) pins that lowercase- or
// mixed-case-named macros parse as UnknownStmt rather than erroring at
// the lexer level. Genuinely-broken input (e.g. "@@@()") still errors.
func TestLowercaseAndMixedCaseMacro(t *testing.T) {
	t.Run("lowercase", func(t *testing.T) {
		mf, err := Parse("test.input", []byte("lowercase_macro()\n"))
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		if len(mf.Stmts) != 1 {
			t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
		}
		u, ok := mf.Stmts[0].(*UnknownStmt)
		if !ok {
			t.Fatalf("Stmts[0] = %T, want *UnknownStmt", mf.Stmts[0])
		}
		if u.Name != "lowercase_macro" {
			t.Errorf("UnknownStmt.Name = %q, want %q", u.Name, "lowercase_macro")
		}
	})
	t.Run("mixed_case_with_args", func(t *testing.T) {
		mf, err := Parse("test.input", []byte(`Mixed_Case(arg1 "arg2")`))
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		if len(mf.Stmts) != 1 {
			t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
		}
		u, ok := mf.Stmts[0].(*UnknownStmt)
		if !ok {
			t.Fatalf("Stmts[0] = %T, want *UnknownStmt", mf.Stmts[0])
		}
		if u.Name != "Mixed_Case" {
			t.Errorf("UnknownStmt.Name = %q, want %q", u.Name, "Mixed_Case")
		}
		want := []string{"arg1", "arg2"}
		if !equalStrings(u.Args, want) {
			t.Errorf("UnknownStmt.Args = %v, want %v", u.Args, want)
		}
	})
	t.Run("garbage_still_errors", func(t *testing.T) {
		_, err := Parse("test.input", []byte("@@@()"))
		if err == nil {
			t.Fatalf("Parse(@@@()): want error, got nil")
		}
	})
}

// TestIsWordByteBoundary (D05) probes the accepted/rejected word-byte
// set so future relaxations are forced through the test gate.
func TestIsWordByteBoundary(t *testing.T) {
	accepted := []byte{
		'a', 'z', 'A', 'Z', '0', '9',
		'_', '-', '.', '/', '+', ':', '=', '*', '?', '$', '%', '~', ',', '!',
		'{', '}', // kept for ${VAR} interpolation
		'#', // word byte mid-word; skipTrivia gates leading '#'
	}
	rejected := []byte{
		' ', '\t', '\n', '\r',
		'(', ')', '"',
		'\'', '`', ';', '|', '&', '^', '<', '>', '[', ']',
		'@',
	}
	for _, b := range accepted {
		if !isWordByte(b) {
			t.Errorf("isWordByte(%q) = false, want true", b)
		}
	}
	for _, b := range rejected {
		if isWordByte(b) {
			t.Errorf("isWordByte(%q) = true, want false", b)
		}
	}
}

// TestMidWordHashIsLiteral (D06) pins that a '#' which appears
// mid-word is part of the word, not the start of a comment that
// swallows the rest of the line up to (and past) the closing ')'.
func TestMidWordHashIsLiteral(t *testing.T) {
	src := []byte("PEERDIR(a/b#x  # this IS a comment\n)\n")
	mf, err := Parse("test.input", src)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mf.Stmts) != 1 {
		t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
	}
	p, ok := mf.Stmts[0].(*PeerdirStmt)
	if !ok {
		t.Fatalf("Stmts[0] = %T, want *PeerdirStmt", mf.Stmts[0])
	}
	want := []string{"a/b#x"}
	if !equalStrings(p.Paths, want) {
		t.Errorf("PEERDIR.Paths = %v, want %v", p.Paths, want)
	}
}

// TestStringHasNoEscapeProcessing (D08) pins the documented behavior
// that string bodies are raw — backslash-X is two literal bytes, not
// an escape sequence. If you ever want to change this, update the
// comment in readString and update this test together.
func TestStringHasNoEscapeProcessing(t *testing.T) {
	src := []byte(`SET(N "ab\X")`)
	mf, err := Parse("test.input", src)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mf.Stmts) != 1 {
		t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
	}
	s, ok := mf.Stmts[0].(*SetStmt)
	if !ok {
		t.Fatalf("Stmts[0] = %T, want *SetStmt", mf.Stmts[0])
	}
	want := `ab\X`
	if s.Value != want {
		t.Errorf("SET.Value = %q (% x), want %q (% x)", s.Value, s.Value, want, want)
	}
	if len(s.Value) != 4 {
		t.Errorf("len(SET.Value) = %d, want 4", len(s.Value))
	}
}

// TestParseIfElseEndif (PR-13) pins the simple two-arm case:
// `IF (FOO) ... ELSE ... ENDIF` produces a single *IfStmt with both
// THEN and ELSE bodies populated.
func TestParseIfElseEndif(t *testing.T) {
	src := []byte(`IF (FOO)
    SRCS(then.cpp)
ELSE()
    SRCS(else.cpp)
ENDIF()
`)

	mf, err := Parse("test.input", src)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mf.Stmts) != 1 {
		t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
	}
	ifStmt, ok := mf.Stmts[0].(*IfStmt)
	if !ok {
		t.Fatalf("Stmts[0] = %T, want *IfStmt", mf.Stmts[0])
	}

	cond, ok := ifStmt.Cond.(*ExprIdent)
	if !ok {
		t.Fatalf("IfStmt.Cond = %T, want *ExprIdent", ifStmt.Cond)
	}
	if cond.Name != "FOO" {
		t.Errorf("ExprIdent.Name = %q, want %q", cond.Name, "FOO")
	}

	if len(ifStmt.Then) != 1 {
		t.Fatalf("len(Then) = %d, want 1", len(ifStmt.Then))
	}
	thenSrc, ok := ifStmt.Then[0].(*SrcsStmt)
	if !ok {
		t.Fatalf("Then[0] = %T, want *SrcsStmt", ifStmt.Then[0])
	}
	if !equalStrings(thenSrc.Sources, []string{"then.cpp"}) {
		t.Errorf("Then SRCS = %v, want [then.cpp]", thenSrc.Sources)
	}

	if len(ifStmt.Else) != 1 {
		t.Fatalf("len(Else) = %d, want 1", len(ifStmt.Else))
	}
	elseSrc, ok := ifStmt.Else[0].(*SrcsStmt)
	if !ok {
		t.Fatalf("Else[0] = %T, want *SrcsStmt", ifStmt.Else[0])
	}
	if !equalStrings(elseSrc.Sources, []string{"else.cpp"}) {
		t.Errorf("Else SRCS = %v, want [else.cpp]", elseSrc.Sources)
	}
}

// TestParseIfElseifEndif (PR-13) pins the chained-ELSEIF case:
// `IF (A) ... ELSEIF (B) ... ELSE ... ENDIF` produces an outer IfStmt
// whose Else body is a single nested IfStmt holding the ELSEIF cond
// and ELSE body. Mirrors C's `else if` chain.
func TestParseIfElseifEndif(t *testing.T) {
	src := []byte(`IF (A)
    SRCS(a.cpp)
ELSEIF (B)
    SRCS(b.cpp)
ELSE()
    SRCS(c.cpp)
ENDIF()
`)

	mf, err := Parse("test.input", src)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mf.Stmts) != 1 {
		t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
	}
	outer, ok := mf.Stmts[0].(*IfStmt)
	if !ok {
		t.Fatalf("Stmts[0] = %T, want *IfStmt", mf.Stmts[0])
	}
	if id, ok := outer.Cond.(*ExprIdent); !ok || id.Name != "A" {
		t.Fatalf("outer.Cond = %#v, want ExprIdent{A}", outer.Cond)
	}

	if len(outer.Else) != 1 {
		t.Fatalf("len(outer.Else) = %d, want 1 (the nested ELSEIF)", len(outer.Else))
	}
	nested, ok := outer.Else[0].(*IfStmt)
	if !ok {
		t.Fatalf("outer.Else[0] = %T, want *IfStmt (the ELSEIF)", outer.Else[0])
	}
	if id, ok := nested.Cond.(*ExprIdent); !ok || id.Name != "B" {
		t.Fatalf("nested.Cond = %#v, want ExprIdent{B}", nested.Cond)
	}
	if len(nested.Then) != 1 {
		t.Fatalf("len(nested.Then) = %d, want 1", len(nested.Then))
	}
	if len(nested.Else) != 1 {
		t.Fatalf("len(nested.Else) = %d, want 1", len(nested.Else))
	}
	finalSrc, ok := nested.Else[0].(*SrcsStmt)
	if !ok {
		t.Fatalf("nested.Else[0] = %T, want *SrcsStmt", nested.Else[0])
	}
	if !equalStrings(finalSrc.Sources, []string{"c.cpp"}) {
		t.Errorf("final ELSE SRCS = %v, want [c.cpp]", finalSrc.Sources)
	}
}

// TestParseInclude_RelativePath (PR-13) pins INCLUDE's inline-expand
// behavior: a parent ya.make with `INCLUDE(sub.inc)` and a sibling
// `sub.inc` containing `SRCS(x.cpp)` parses into a Stmts slice that
// CONTAINS the SrcsStmt and does NOT contain an IncludeStmt
// (Parse/ParseFile drop the marker).
func TestParseInclude_RelativePath(t *testing.T) {
	dir := t.TempDir()

	parentPath := filepath.Join(dir, "ya.make")
	subPath := filepath.Join(dir, "sub.inc")

	if err := os.WriteFile(parentPath, []byte("LIBRARY()\nINCLUDE(sub.inc)\nEND()\n"), 0o644); err != nil {
		t.Fatalf("write parent: %v", err)
	}
	if err := os.WriteFile(subPath, []byte("SRCS(x.cpp)\n"), 0o644); err != nil {
		t.Fatalf("write sub: %v", err)
	}

	mf, err := ParseFile(parentPath)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	for _, s := range mf.Stmts {
		if _, isInc := s.(*IncludeStmt); isInc {
			t.Errorf("Stmts contains *IncludeStmt; expected it to be dropped after inline expansion")
		}
	}

	var srcs *SrcsStmt
	for _, s := range mf.Stmts {
		if v, ok := s.(*SrcsStmt); ok {
			srcs = v
		}
	}
	if srcs == nil {
		t.Fatalf("Stmts has no *SrcsStmt; got %#v", mf.Stmts)
	}
	if !equalStrings(srcs.Sources, []string{"x.cpp"}) {
		t.Errorf("included SRCS = %v, want [x.cpp]", srcs.Sources)
	}
}

// TestParseJoinSrcs pins `JOIN_SRCS(name srcs...)` parsing.
func TestParseJoinSrcs(t *testing.T) {
	mf, err := Parse("test.input", []byte("JOIN_SRCS(allfoo a.cpp b.cpp)\n"))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mf.Stmts) != 1 {
		t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
	}
	js, ok := mf.Stmts[0].(*JoinSrcsStmt)
	if !ok {
		t.Fatalf("Stmts[0] = %T, want *JoinSrcsStmt", mf.Stmts[0])
	}
	if js.OutputName != "allfoo" {
		t.Errorf("OutputName = %q, want %q", js.OutputName, "allfoo")
	}
	if !equalStrings(js.Sources, []string{"a.cpp", "b.cpp"}) {
		t.Errorf("Sources = %v, want [a.cpp b.cpp]", js.Sources)
	}
}

// TestParseJoinSrcs_RejectsEmpty pins that JOIN_SRCS with zero args
// throws — at minimum the output name is required.
func TestParseJoinSrcs_RejectsEmpty(t *testing.T) {
	_, err := Parse("test.input", []byte("JOIN_SRCS()\n"))
	if err == nil {
		t.Fatal("Parse returned nil error, want *ParseError")
	}
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("Parse returned %T, want *ParseError", err)
	}
	if !strings.Contains(pe.Message, "JOIN_SRCS") {
		t.Errorf("ParseError.Message = %q, want it to mention JOIN_SRCS", pe.Message)
	}
}

// TestParseAddIncl_Global pins ADDINCL with the GLOBAL modifier.
func TestParseAddIncl_Global(t *testing.T) {
	mf, err := Parse("test.input", []byte("ADDINCL(GLOBAL include1 include2)\n"))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mf.Stmts) != 1 {
		t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
	}
	a, ok := mf.Stmts[0].(*AddInclStmt)
	if !ok {
		t.Fatalf("Stmts[0] = %T, want *AddInclStmt", mf.Stmts[0])
	}
	if a.Modifier != "GLOBAL" {
		t.Errorf("Modifier = %q, want %q", a.Modifier, "GLOBAL")
	}
	if !equalStrings(a.Paths, []string{"include1", "include2"}) {
		t.Errorf("Paths = %v, want [include1 include2]", a.Paths)
	}
}

// TestParseAddIncl_NoModifier pins ADDINCL without the GLOBAL prefix.
func TestParseAddIncl_NoModifier(t *testing.T) {
	mf, err := Parse("test.input", []byte("ADDINCL(include1)\n"))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	a, ok := mf.Stmts[0].(*AddInclStmt)
	if !ok {
		t.Fatalf("Stmts[0] = %T, want *AddInclStmt", mf.Stmts[0])
	}
	if a.Modifier != "" {
		t.Errorf("Modifier = %q, want empty", a.Modifier)
	}
	if !equalStrings(a.Paths, []string{"include1"}) {
		t.Errorf("Paths = %v, want [include1]", a.Paths)
	}
}

// TestParseCFlags_Global pins CFLAGS with the GLOBAL modifier.
func TestParseCFlags_Global(t *testing.T) {
	mf, err := Parse("test.input", []byte("CFLAGS(GLOBAL -O2 -Wall)\n"))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	c, ok := mf.Stmts[0].(*CFlagsStmt)
	if !ok {
		t.Fatalf("Stmts[0] = %T, want *CFlagsStmt", mf.Stmts[0])
	}
	if c.Modifier != "GLOBAL" {
		t.Errorf("Modifier = %q, want %q", c.Modifier, "GLOBAL")
	}
	if !equalStrings(c.Flags, []string{"-O2", "-Wall"}) {
		t.Errorf("Flags = %v, want [-O2 -Wall]", c.Flags)
	}
}

// TestParseCFlags_NoModifier pins CFLAGS without the GLOBAL prefix.
func TestParseCFlags_NoModifier(t *testing.T) {
	mf, err := Parse("test.input", []byte("CFLAGS(-O2)\n"))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	c, ok := mf.Stmts[0].(*CFlagsStmt)
	if !ok {
		t.Fatalf("Stmts[0] = %T, want *CFlagsStmt", mf.Stmts[0])
	}
	if c.Modifier != "" {
		t.Errorf("Modifier = %q, want empty", c.Modifier)
	}
	if !equalStrings(c.Flags, []string{"-O2"}) {
		t.Errorf("Flags = %v, want [-O2]", c.Flags)
	}
}

// TestParseLDFlags pins LDFLAGS — no modifier, just a flat flag list.
func TestParseLDFlags(t *testing.T) {
	mf, err := Parse("test.input", []byte("LDFLAGS(-lpthread -lm)\n"))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	l, ok := mf.Stmts[0].(*LDFlagsStmt)
	if !ok {
		t.Fatalf("Stmts[0] = %T, want *LDFlagsStmt", mf.Stmts[0])
	}
	if !equalStrings(l.Flags, []string{"-lpthread", "-lm"}) {
		t.Errorf("Flags = %v, want [-lpthread -lm]", l.Flags)
	}
}

// TestParseSrcDir pins SRCDIR(dir) — single-arg, exposes the path.
func TestParseSrcDir(t *testing.T) {
	mf, err := Parse("test.input", []byte("SRCDIR(./xx)\n"))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	s, ok := mf.Stmts[0].(*SrcDirStmt)
	if !ok {
		t.Fatalf("Stmts[0] = %T, want *SrcDirStmt", mf.Stmts[0])
	}
	if s.Dir != "./xx" {
		t.Errorf("Dir = %q, want %q", s.Dir, "./xx")
	}
}

// TestParseGlobalSrcs pins GLOBAL_SRCS — flat source list, no
// modifier.
func TestParseGlobalSrcs(t *testing.T) {
	mf, err := Parse("test.input", []byte("GLOBAL_SRCS(a.cpp b.cpp c.cpp)\n"))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	g, ok := mf.Stmts[0].(*GlobalSrcsStmt)
	if !ok {
		t.Fatalf("Stmts[0] = %T, want *GlobalSrcsStmt", mf.Stmts[0])
	}
	if !equalStrings(g.Sources, []string{"a.cpp", "b.cpp", "c.cpp"}) {
		t.Errorf("Sources = %v, want [a.cpp b.cpp c.cpp]", g.Sources)
	}
}

// TestParseInclude_RejectsSelfCycle (PR-13-D01) pins that a self-referential
// INCLUDE(a.inc) is caught as a cycle before the goroutine stack overflows.
func TestParseInclude_RejectsSelfCycle(t *testing.T) {
	tmp := t.TempDir()
	Throw(os.WriteFile(filepath.Join(tmp, "a.inc"), []byte("INCLUDE(a.inc)\n"), 0644))

	_, err := ParseFile(filepath.Join(tmp, "a.inc"))

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "INCLUDE cycle") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestParseInclude_RejectsTransitiveCycle (PR-13-D01) pins that a two-hop
// cycle (a.inc → b.inc → a.inc) is also caught before the goroutine stack
// overflows.
func TestParseInclude_RejectsTransitiveCycle(t *testing.T) {
	tmp := t.TempDir()
	Throw(os.WriteFile(filepath.Join(tmp, "a.inc"), []byte("INCLUDE(b.inc)\n"), 0644))
	Throw(os.WriteFile(filepath.Join(tmp, "b.inc"), []byte("INCLUDE(a.inc)\n"), 0644))

	_, err := ParseFile(filepath.Join(tmp, "a.inc"))

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "INCLUDE cycle") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestParseIf_StringEquality (PR-27) pins the parser handling of
// `IF (X == "Y")` — the libcxx pattern. The condition expression
// is an *ExprEq with an *ExprIdent left and an *ExprString right;
// both arms parse cleanly and the THEN body's SRCS is preserved.
func TestParseIf_StringEquality(t *testing.T) {
	src := []byte(`IF (CXX_RT == "libcxxrt")
    SRCS(rt.c)
ENDIF()
`)

	mf, err := Parse("test.input", src)

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(mf.Stmts) != 1 {
		t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
	}

	ifStmt, ok := mf.Stmts[0].(*IfStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *IfStmt", mf.Stmts[0])
	}

	eq, ok := ifStmt.Cond.(*ExprEq)

	if !ok {
		t.Fatalf("IfStmt.Cond = %T, want *ExprEq", ifStmt.Cond)
	}

	if id, ok := eq.Left.(*ExprIdent); !ok || id.Name != "CXX_RT" {
		t.Errorf("ExprEq.Left = %#v, want ExprIdent{CXX_RT}", eq.Left)
	}

	if s, ok := eq.Right.(*ExprString); !ok || s.Value != "libcxxrt" {
		t.Errorf("ExprEq.Right = %#v, want ExprString{libcxxrt}", eq.Right)
	}
}

// TestParseIf_NumericLessThan (PR-27) pins the parser handling of
// `IF (X < N)` — the libc_compat ANDROID_API pattern.
func TestParseIf_NumericLessThan(t *testing.T) {
	src := []byte(`IF (ANDROID_API < 28)
    SRCS(android.c)
ENDIF()
`)

	mf, err := Parse("test.input", src)

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(mf.Stmts) != 1 {
		t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
	}

	ifStmt, ok := mf.Stmts[0].(*IfStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *IfStmt", mf.Stmts[0])
	}

	lt, ok := ifStmt.Cond.(*ExprLt)

	if !ok {
		t.Fatalf("IfStmt.Cond = %T, want *ExprLt", ifStmt.Cond)
	}

	if id, ok := lt.Left.(*ExprIdent); !ok || id.Name != "ANDROID_API" {
		t.Errorf("ExprLt.Left = %#v, want ExprIdent{ANDROID_API}", lt.Left)
	}

	if i, ok := lt.Right.(*ExprInt); !ok || i.Value != 28 {
		t.Errorf("ExprLt.Right = %#v, want ExprInt{28}", lt.Right)
	}
}

// TestParseIf_NotEqualDesugars (PR-27) pins the desugar of `X != Y`
// to `NOT (X == Y)` at parse time — the libc_compat OS_SDK pattern.
func TestParseIf_NotEqualDesugars(t *testing.T) {
	src := []byte(`IF (OS_SDK != "ubuntu-20")
    SRCS(other_sdk.c)
ENDIF()
`)

	mf, err := Parse("test.input", src)

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	ifStmt, ok := mf.Stmts[0].(*IfStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *IfStmt", mf.Stmts[0])
	}

	not, ok := ifStmt.Cond.(*ExprNot)

	if !ok {
		t.Fatalf("IfStmt.Cond = %T, want *ExprNot (X != Y desugar)", ifStmt.Cond)
	}

	eq, ok := not.Of.(*ExprEq)

	if !ok {
		t.Fatalf("ExprNot.Of = %T, want *ExprEq", not.Of)
	}

	if id, ok := eq.Left.(*ExprIdent); !ok || id.Name != "OS_SDK" {
		t.Errorf("ExprEq.Left = %#v, want ExprIdent{OS_SDK}", eq.Left)
	}

	if s, ok := eq.Right.(*ExprString); !ok || s.Value != "ubuntu-20" {
		t.Errorf("ExprEq.Right = %#v, want ExprString{ubuntu-20}", eq.Right)
	}
}

// TestParseIf_ChainedComparisonRejected (PR-27) pins the
// non-associativity of comparators: `A == B == C` must be a syntax
// error, not silently associated. The error pinpoints the second
// `==` so the user knows which one is the chain offender.
func TestParseIf_ChainedComparisonRejected(t *testing.T) {
	_, err := Parse("test.input", []byte("IF (A == B == C)\nENDIF()\n"))

	if err == nil {
		t.Fatal("expected error for chained comparison, got nil")
	}

	if !strings.Contains(err.Error(), "chained comparison") {
		t.Errorf("error %q does not mention 'chained comparison'", err.Error())
	}
}

// TestParseIf_ComparisonInAndOr (PR-27) pins that comparisons bind
// tighter than AND / OR — the libcxxrt pattern
// `IF (SANITIZER_TYPE == undefined OR FUZZING)` must parse as
// `(SANITIZER_TYPE == undefined) OR FUZZING`, not as `SANITIZER_TYPE
// == (undefined OR FUZZING)`.
func TestParseIf_ComparisonInAndOr(t *testing.T) {
	src := []byte(`IF (SANITIZER_TYPE == undefined OR FUZZING)
    SRCS(x.c)
ENDIF()
`)

	mf, err := Parse("test.input", src)

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	ifStmt := mf.Stmts[0].(*IfStmt)

	or, ok := ifStmt.Cond.(*ExprOr)

	if !ok {
		t.Fatalf("Cond = %T, want *ExprOr (OR is outermost)", ifStmt.Cond)
	}

	if _, ok := or.Left.(*ExprEq); !ok {
		t.Errorf("OR.Left = %T, want *ExprEq", or.Left)
	}

	if id, ok := or.Right.(*ExprIdent); !ok || id.Name != "FUZZING" {
		t.Errorf("OR.Right = %#v, want ExprIdent{FUZZING}", or.Right)
	}
}

// TestParseIf_VersionLiteralStillWord (PR-27) pins the lexer's
// readNumberOrWord regression check: `VERSION(2025-06-20)` is a
// macro arg that begins with a digit but contains non-digit word
// bytes; it must still lex as a single tokWord, not be split into
// tokInt + tokWord by the new digit-leading path.
func TestParseIf_VersionLiteralStillWord(t *testing.T) {
	mf, err := Parse("test.input", []byte("VERSION(2025-06-20)\n"))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(mf.Stmts) != 1 {
		t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
	}

	u, ok := mf.Stmts[0].(*UnknownStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *UnknownStmt", mf.Stmts[0])
	}

	if !equalStrings(u.Args, []string{"2025-06-20"}) {
		t.Errorf("VERSION args = %v, want [2025-06-20]", u.Args)
	}
}

// TestParseIf_PureIntInMacroArg (PR-27) pins that a pure-digit
// macro arg lexes as tokInt and is preserved verbatim when surfaced
// as a string in UnknownStmt.Args (e.g. `IDE_FOLDER(42)`).
func TestParseIf_PureIntInMacroArg(t *testing.T) {
	mf, err := Parse("test.input", []byte("IDE_FOLDER(42)\n"))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	u := mf.Stmts[0].(*UnknownStmt)

	if !equalStrings(u.Args, []string{"42"}) {
		t.Errorf("IDE_FOLDER args = %v, want [42]", u.Args)
	}
}

// equalStrings is a tiny helper to keep test output readable.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// equalInts is a tiny helper for integer-slice comparison in tests.
func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
