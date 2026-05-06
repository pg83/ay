package main

import (
	"errors"
	"io/fs"
	"os"
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
