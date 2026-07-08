package main

import (
	"errors"
	"strings"
	"testing"
)

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
	mf, err := parse(testParserFS, "tools/archiver/ya.make", []byte(archiverYaMake))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if got, want := len(mf.Stmts), 5; got != want {
		t.Fatalf("len(Stmts) = %d, want %d (stmts=%#v)", got, want, mf.Stmts)
	}

	if m, ok := mf.Stmts[0].(*ModuleStmt); !ok {
		t.Fatalf("Stmts[0] = %T, want *ModuleStmt", mf.Stmts[0])
	} else {
		if m.Name != tokProgram {
			t.Errorf("Stmts[0].Name = %q, want %q", m.Name, "PROGRAM")
		}

		if len(m.Args) != 0 {
			t.Errorf("Stmts[0].Args = %v, want empty", m.Args)
		}

		if m.Line == 0 {
			t.Errorf("Stmts[0].Line = 0, want non-zero")
		}
	}

	if p, ok := mf.Stmts[1].(*PeerdirStmt); !ok {
		t.Fatalf("Stmts[1] = %T, want *PeerdirStmt", mf.Stmts[1])
	} else {
		want := []string{"library/cpp/archive", "library/cpp/digest/md5", "library/cpp/getopt/small"}

		if !equalStrings(anyStrs(p.Paths), want) {
			t.Errorf("PEERDIR.Paths = %v, want %v", p.Paths, want)
		}
	}

	if s, ok := mf.Stmts[2].(*SrcsStmt); !ok {
		t.Fatalf("Stmts[2] = %T, want *SrcsStmt", mf.Stmts[2])
	} else {
		want := []string{"main.cpp"}

		if !equalStrings(anyStrs(s.Sources), want) {
			t.Errorf("SRCS.Sources = %v, want %v", s.Sources, want)
		}
	}

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

	if _, ok := mf.Stmts[4].(*EndStmt); !ok {
		t.Fatalf("Stmts[4] = %T, want *EndStmt", mf.Stmts[4])
	}
}

func TestParseLibraryArchiveYaMake(t *testing.T) {
	mf, err := parse(testParserFS, "library/cpp/archive/ya.make", []byte(libraryArchiveYaMake))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	sawLibrary := false
	sawEnd := false

	for _, s := range mf.Stmts {
		switch v := s.(type) {
		case *ModuleStmt:
			if v.Name == tokLibrary {
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

func TestUnknownMacro(t *testing.T) {
	src := []byte("NO_LINT(foo bar)\n")
	mf, err := parse(testParserFS, "test.input", src)

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

	if u.Name != tokNoLint {
		t.Errorf("UnknownStmt.Name = %q, want %q", u.Name, tokNoLint)
	}

	want := []string{"foo", "bar"}

	if !equalStrings(anyStrs(u.Args), want) {
		t.Errorf("UnknownStmt.Args = %v, want %v", u.Args, want)
	}
}

func TestUnknownMacroNameFailsFast(t *testing.T) {
	if _, err := parse(testParserFS, "test.input", []byte("FROBNICATE(foo bar)\n")); err == nil {
		t.Fatal("Parse accepted an unknown macro name; want fail-fast error")
	}
}

func TestCommentHandling(t *testing.T) {
	src := []byte("# this is a comment\nPROGRAM()\n")
	mf, err := parse(testParserFS, "test.input", src)

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

	if m.Name != tokProgram {
		t.Errorf("ModuleStmt.Name = %q, want %q", m.Name, "PROGRAM")
	}

	if m.Line != 2 {
		t.Errorf("PROGRAM.Line = %d, want 2", m.Line)
	}
}

func TestMultilineMacro(t *testing.T) {
	src := []byte("PEERDIR(\n  a/b\n  c/d\n)\n")
	mf, err := parse(testParserFS, "test.input", src)

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

	if !equalStrings(anyStrs(p.Paths), want) {
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
		{"single-quoted", `SET(IDE_FOLDER '_Builders')`, "IDE_FOLDER", "_Builders"},
		{"unquoted", `SET(NAME bare_value)`, "NAME", "bare_value"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mf, err := parse(testParserFS, "test.input", []byte(tc.src))

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
		wantSubstr  string
		wantNonZero bool
	}{
		{"unterminated string", `"hello`, "unterminated string", true},
		{"unterminated macro", `PROGRAM(`, "unterminated macro", true},
		{"weird character", `@@@`, "unexpected character", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parse(testParserFS, "test.input", []byte(tc.src))

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

func TestSetAllowsEmptyValue(t *testing.T) {
	mf, err := parse(testParserFS, "test.input", []byte(`SET(only_one_arg)`))

	if err != nil {
		t.Fatalf("Parse returned %v, want success", err)
	}

	if len(mf.Stmts) != 1 {
		t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
	}

	stmt, ok := mf.Stmts[0].(*SetStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *SetStmt", mf.Stmts[0])
	}

	if stmt.Name != "only_one_arg" {
		t.Fatalf("SetStmt.Name = %q, want only_one_arg", stmt.Name)
	}

	if stmt.Value != "" {
		t.Fatalf("SetStmt.Value = %q, want empty", stmt.Value)
	}
}

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
			mf, err := parse(testParserFS, "test.input", []byte(tc.src))

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
			_, err := parse(testParserFS, "test.input", []byte(tc.src))

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

			if pe.Line != 1 {
				t.Errorf("ParseError.Line = %d, want 1 (the opening-quote line)", pe.Line)
			}
		})
	}
}

func TestLowercaseAndMixedCaseMacro(t *testing.T) {
	t.Run("lowercase", func(t *testing.T) {
		if _, err := parse(testParserFS, "test.input", []byte("lowercase_macro()\n")); err == nil {
			t.Fatal("Parse accepted lowercase non-TOK macro; want fail-fast error")
		}
	})
	t.Run("mixed_case_with_args", func(t *testing.T) {
		if _, err := parse(testParserFS, "test.input", []byte(`Mixed_Case(arg1 "arg2")`)); err == nil {
			t.Fatal("Parse accepted mixed-case non-TOK macro; want fail-fast error")
		}
	})
	t.Run("garbage_still_errors", func(t *testing.T) {
		_, err := parse(testParserFS, "test.input", []byte("@@@()"))

		if err == nil {
			t.Fatalf("Parse(@@@()): want error, got nil")
		}
	})
}

func TestIsWordByteBoundary(t *testing.T) {
	accepted := []byte{
		'a', 'z', 'A', 'Z', '0', '9',
		'_', '-', '.', '/', '+', ':', '=', '*', '?', '$', '%', '~', ',', '!',
		'{', '}', '#', '[', ']',
	}
	rejected := []byte{
		' ', '\t', '\n', '\r',
		'(', ')', '"',
		'\'', '`', ';', '|', '&', '^', '<', '>',
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

func TestMidWordHashIsLiteral(t *testing.T) {
	src := []byte("PEERDIR(a/b#x  # this IS a comment\n)\n")
	mf, err := parse(testParserFS, "test.input", src)

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

	if !equalStrings(anyStrs(p.Paths), want) {
		t.Errorf("PEERDIR.Paths = %v, want %v", p.Paths, want)
	}
}

func TestStringHasNoEscapeProcessing(t *testing.T) {
	src := []byte(`SET(N "ab\X")`)
	mf, err := parse(testParserFS, "test.input", src)

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

func TestParseIfElseEndif(t *testing.T) {
	src := []byte(`IF (FOO)
    SRCS(then.cpp)
ELSE()
    SRCS(else.cpp)
ENDIF()
`)

	mf, err := parse(testParserFS, "test.input", src)

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

	cond := condRoot(ifStmt.Cond)

	if cond.Kind != ckIdent {
		t.Fatalf("IfStmt.Cond root = kind %d, want ckIdent", cond.Kind)
	}

	if cond.Name != "FOO" {
		t.Errorf("cond ident Name = %q, want %q", cond.Name, "FOO")
	}

	if len(ifStmt.Then) != 1 {
		t.Fatalf("len(Then) = %d, want 1", len(ifStmt.Then))
	}

	thenSrc, ok := ifStmt.Then[0].(*SrcsStmt)

	if !ok {
		t.Fatalf("Then[0] = %T, want *SrcsStmt", ifStmt.Then[0])
	}

	if !equalStrings(anyStrs(thenSrc.Sources), []string{"then.cpp"}) {
		t.Errorf("Then SRCS = %v, want [then.cpp]", thenSrc.Sources)
	}

	if len(ifStmt.Else) != 1 {
		t.Fatalf("len(Else) = %d, want 1", len(ifStmt.Else))
	}

	elseSrc, ok := ifStmt.Else[0].(*SrcsStmt)

	if !ok {
		t.Fatalf("Else[0] = %T, want *SrcsStmt", ifStmt.Else[0])
	}

	if !equalStrings(anyStrs(elseSrc.Sources), []string{"else.cpp"}) {
		t.Errorf("Else SRCS = %v, want [else.cpp]", elseSrc.Sources)
	}
}

func TestParseIfElseifEndif(t *testing.T) {
	src := []byte(`IF (A)
    SRCS(a.cpp)
ELSEIF (B)
    SRCS(b.cpp)
ELSE()
    SRCS(c.cpp)
ENDIF()
`)

	mf, err := parse(testParserFS, "test.input", src)

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

	if root := condRoot(outer.Cond); root.Kind != ckIdent || root.Name != "A" {
		t.Fatalf("outer.Cond root = %+v, want ident{A}", root)
	}

	if len(outer.Else) != 1 {
		t.Fatalf("len(outer.Else) = %d, want 1 (the nested ELSEIF)", len(outer.Else))
	}

	nested, ok := outer.Else[0].(*IfStmt)

	if !ok {
		t.Fatalf("outer.Else[0] = %T, want *IfStmt (the ELSEIF)", outer.Else[0])
	}

	if root := condRoot(nested.Cond); root.Kind != ckIdent || root.Name != "B" {
		t.Fatalf("nested.Cond root = %+v, want ident{B}", root)
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

	if !equalStrings(anyStrs(finalSrc.Sources), []string{"c.cpp"}) {
		t.Errorf("final ELSE SRCS = %v, want [c.cpp]", finalSrc.Sources)
	}
}

func TestParseInclude_RelativePath(t *testing.T) {
	fs := newMemFS(map[string]string{
		"ya.make": "LIBRARY()\nINCLUDE(sub.inc)\nEND()\n",
		"sub.inc": "SRCS(x.cpp)\n",
	})

	mf, err := parseFile(fs, "ya.make")

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

	if !equalStrings(anyStrs(srcs.Sources), []string{"x.cpp"}) {
		t.Errorf("included SRCS = %v, want [x.cpp]", srcs.Sources)
	}
}

func TestParseJoinSrcs(t *testing.T) {
	mf, err := parse(testParserFS, "test.input", []byte("JOIN_SRCS(allfoo a.cpp b.cpp)\n"))

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

	if !equalStrings(anyStrs(js.Sources), []string{"a.cpp", "b.cpp"}) {
		t.Errorf("Sources = %v, want [a.cpp b.cpp]", js.Sources)
	}
}

func TestParseJoinSrcs_RejectsEmpty(t *testing.T) {
	_, err := parse(testParserFS, "test.input", []byte("JOIN_SRCS()\n"))

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

func TestParseAddIncl_AllGlobal(t *testing.T) {
	mf, err := parse(testParserFS, "test.input", []byte("ADDINCL(GLOBAL include1 GLOBAL include2)\n"))

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

	if !equalStrings(anyStrs(a.GlobalPaths), []string{"include1", "include2"}) {
		t.Errorf("GlobalPaths = %v, want [include1 include2]", a.GlobalPaths)
	}

	if len(a.OwnPaths) != 0 {
		t.Errorf("OwnPaths = %v, want empty", a.OwnPaths)
	}
}

func TestParseAddIncl_NoGlobal(t *testing.T) {
	mf, err := parse(testParserFS, "test.input", []byte("ADDINCL(include1)\n"))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	a, ok := mf.Stmts[0].(*AddInclStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *AddInclStmt", mf.Stmts[0])
	}

	if len(a.GlobalPaths) != 0 {
		t.Errorf("GlobalPaths = %v, want empty", a.GlobalPaths)
	}

	if !equalStrings(anyStrs(a.OwnPaths), []string{"include1"}) {
		t.Errorf("OwnPaths = %v, want [include1]", a.OwnPaths)
	}
}

func TestParseAddIncl_Mixed(t *testing.T) {
	src := "ADDINCL(\n    GLOBAL libcxx/include\n    libcxx/src\n)\n"
	mf, err := parse(testParserFS, "test.input", []byte(src))

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

	if !equalStrings(anyStrs(a.GlobalPaths), []string{"libcxx/include"}) {
		t.Errorf("GlobalPaths = %v, want [libcxx/include]", a.GlobalPaths)
	}

	if !equalStrings(anyStrs(a.OwnPaths), []string{"libcxx/src"}) {
		t.Errorf("OwnPaths = %v, want [libcxx/src]", a.OwnPaths)
	}
}

func TestParseAddIncl_ForKindDropped(t *testing.T) {
	mf, err := parse(testParserFS, "test.input", []byte("ADDINCL(FOR proto contrib/libs/protobuf/src)\n"))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	a, ok := mf.Stmts[0].(*AddInclStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *AddInclStmt", mf.Stmts[0])
	}

	if len(a.GlobalPaths) != 0 {
		t.Errorf("GlobalPaths = %v, want empty", a.GlobalPaths)
	}

	if !equalStrings(anyStrs(a.OwnPaths), []string{"contrib/libs/protobuf/src"}) {
		t.Errorf("OwnPaths = %v, want [contrib/libs/protobuf/src]", a.OwnPaths)
	}
}

func TestParseAddIncl_GlobalForProtoRouted(t *testing.T) {
	src := "ADDINCL(\n    GLOBAL contrib/libs/protobuf/src\n    GLOBAL FOR\n    proto\n    contrib/libs/protobuf/src\n)\n"
	mf, err := parse(testParserFS, "test.input", []byte(src))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	a, ok := mf.Stmts[0].(*AddInclStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *AddInclStmt", mf.Stmts[0])
	}

	if !equalStrings(anyStrs(a.GlobalPaths), []string{"contrib/libs/protobuf/src"}) {
		t.Errorf("GlobalPaths = %v, want [contrib/libs/protobuf/src]", a.GlobalPaths)
	}

	if !equalStrings(anyStrs(a.ProtoGlobalPaths), []string{"contrib/libs/protobuf/src"}) {
		t.Errorf("ProtoGlobalPaths = %v, want [contrib/libs/protobuf/src]", a.ProtoGlobalPaths)
	}

	if len(a.OwnPaths) != 0 {
		t.Errorf("OwnPaths = %v, want empty", a.OwnPaths)
	}
}

func TestParseAddIncl_ForAsmRouted(t *testing.T) {
	mf, err := parse(testParserFS, "test.input",
		[]byte("ADDINCL(FOR asm yt/yt/core/misc/isa_crc64/include)\n"))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	a, ok := mf.Stmts[0].(*AddInclStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *AddInclStmt", mf.Stmts[0])
	}

	if !equalStrings(anyStrs(a.AsmPaths), []string{"yt/yt/core/misc/isa_crc64/include"}) {
		t.Errorf("AsmPaths = %v, want [yt/yt/core/misc/isa_crc64/include]", a.AsmPaths)
	}

	if len(a.OwnPaths) != 0 {
		t.Errorf("OwnPaths = %v, want empty", a.OwnPaths)
	}

	if len(a.GlobalPaths) != 0 {
		t.Errorf("GlobalPaths = %v, want empty", a.GlobalPaths)
	}

	if len(a.AllPaths) != 0 {
		t.Errorf("AllPaths = %v, want empty", a.AllPaths)
	}
}

func TestParseAddIncl_GlobalForAsmRouted(t *testing.T) {
	src := "ADDINCL(\n    GLOBAL FOR\n    asm\n    yt/yt/core/misc/isa_crc64/include\n)\n"
	mf, err := parse(testParserFS, "test.input", []byte(src))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	a, ok := mf.Stmts[0].(*AddInclStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *AddInclStmt", mf.Stmts[0])
	}

	if !equalStrings(anyStrs(a.AsmPaths), []string{"yt/yt/core/misc/isa_crc64/include"}) {
		t.Errorf("AsmPaths = %v, want [yt/yt/core/misc/isa_crc64/include]", a.AsmPaths)
	}

	if len(a.GlobalPaths) != 0 {
		t.Errorf("GlobalPaths = %v, want empty", a.GlobalPaths)
	}

	if len(a.AllPaths) != 0 {
		t.Errorf("AllPaths = %v, want empty", a.AllPaths)
	}
}

func TestParseCFlags_BackslashQuoteUnescaped(t *testing.T) {
	mf, err := parse(testParserFS, "test.input", []byte("CFLAGS(-DENGINESDIR=\\\"/usr/local/lib/engines-1.1\\\")\n"))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	c, ok := mf.Stmts[0].(*CFlagsStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *CFlagsStmt", mf.Stmts[0])
	}

	want := `-DENGINESDIR="/usr/local/lib/engines-1.1"`

	if len(c.OwnFlags) != 1 || c.OwnFlags[0].string() != want {
		t.Errorf("OwnFlags = %v, want [%s]", c.OwnFlags, want)
	}
}

func TestParseCXXFlags_AdjacentQuotedSuffixStaysSingleFlag(t *testing.T) {
	mf, err := parse(testParserFS, "test.input",
		[]byte("CXXFLAGS(-DYTPROF_BUILD_TYPE='\\\"${BUILD_TYPE}\\\"')\n"))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	c, ok := mf.Stmts[0].(*CXXFlagsStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *CXXFlagsStmt", mf.Stmts[0])
	}

	want := `-DYTPROF_BUILD_TYPE="${BUILD_TYPE}"`

	if len(c.OwnFlags) != 1 || c.OwnFlags[0].string() != want {
		t.Errorf("OwnFlags = %v, want [%s]", c.OwnFlags, want)
	}
}

func TestParseCFlags_Global(t *testing.T) {
	mf, err := parse(testParserFS, "test.input", []byte("CFLAGS(GLOBAL -O2 -Wall)\n"))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	c, ok := mf.Stmts[0].(*CFlagsStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *CFlagsStmt", mf.Stmts[0])
	}

	if !equalStrings(anyStrs(c.GlobalFlags), []string{"-O2"}) {
		t.Errorf("GlobalFlags = %v, want [-O2]", c.GlobalFlags)
	}

	if !equalStrings(anyStrs(c.OwnFlags), []string{"-Wall"}) {
		t.Errorf("OwnFlags = %v, want [-Wall]", c.OwnFlags)
	}
}

func TestParseCFlags_NoModifier(t *testing.T) {
	mf, err := parse(testParserFS, "test.input", []byte("CFLAGS(-O2)\n"))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	c, ok := mf.Stmts[0].(*CFlagsStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *CFlagsStmt", mf.Stmts[0])
	}

	if len(c.GlobalFlags) != 0 {
		t.Errorf("GlobalFlags = %v, want empty", c.GlobalFlags)
	}

	if !equalStrings(anyStrs(c.OwnFlags), []string{"-O2"}) {
		t.Errorf("OwnFlags = %v, want [-O2]", c.OwnFlags)
	}
}

func TestParseCFlags_PerPathGlobal(t *testing.T) {
	mf, err := parse(testParserFS, "test.input", []byte("CFLAGS(GLOBAL -DA -DB GLOBAL -DC)\n"))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	c, ok := mf.Stmts[0].(*CFlagsStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *CFlagsStmt", mf.Stmts[0])
	}

	if !equalStrings(anyStrs(c.GlobalFlags), []string{"-DA", "-DC"}) {
		t.Errorf("GlobalFlags = %v, want [-DA -DC]", c.GlobalFlags)
	}

	if !equalStrings(anyStrs(c.OwnFlags), []string{"-DB"}) {
		t.Errorf("OwnFlags = %v, want [-DB]", c.OwnFlags)
	}
}

func TestParseCXXFlags_Global(t *testing.T) {
	mf, err := parse(testParserFS, "test.input", []byte("CXXFLAGS(GLOBAL -nostdinc++)\n"))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	c, ok := mf.Stmts[0].(*CXXFlagsStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *CXXFlagsStmt", mf.Stmts[0])
	}

	if !equalStrings(anyStrs(c.GlobalFlags), []string{"-nostdinc++"}) {
		t.Errorf("GlobalFlags = %v, want [-nostdinc++]", c.GlobalFlags)
	}

	if len(c.OwnFlags) != 0 {
		t.Errorf("OwnFlags = %v, want empty", c.OwnFlags)
	}
}

func TestParseCONLYFlags_Own(t *testing.T) {
	mf, err := parse(testParserFS, "test.input", []byte("CONLYFLAGS(-Wno-pointer-sign)\n"))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	c, ok := mf.Stmts[0].(*CONLYFlagsStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *CONLYFlagsStmt", mf.Stmts[0])
	}

	if len(c.GlobalFlags) != 0 {
		t.Errorf("GlobalFlags = %v, want empty", c.GlobalFlags)
	}

	if !equalStrings(anyStrs(c.OwnFlags), []string{"-Wno-pointer-sign"}) {
		t.Errorf("OwnFlags = %v, want [-Wno-pointer-sign]", c.OwnFlags)
	}
}

func TestParseLDFlags(t *testing.T) {
	mf, err := parse(testParserFS, "test.input", []byte("LDFLAGS(-lpthread -lm)\n"))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	l, ok := mf.Stmts[0].(*LDFlagsStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *LDFlagsStmt", mf.Stmts[0])
	}

	if !equalStrings(anyStrs(l.Flags), []string{"-lpthread", "-lm"}) {
		t.Errorf("Flags = %v, want [-lpthread -lm]", l.Flags)
	}
}

func TestParseSrcDir(t *testing.T) {
	mf, err := parse(testParserFS, "test.input", []byte("SRCDIR(./xx)\n"))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	s, ok := mf.Stmts[0].(*SrcDirStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *SrcDirStmt", mf.Stmts[0])
	}

	if len(s.Dirs) != 1 || s.Dirs[0].string() != "./xx" {
		t.Errorf("Dirs = %q, want [./xx]", s.Dirs)
	}
}

func TestParseGlobalSrcs(t *testing.T) {
	mf, err := parse(testParserFS, "test.input", []byte("GLOBAL_SRCS(a.cpp b.cpp c.cpp)\n"))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	g, ok := mf.Stmts[0].(*GlobalSrcsStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *GlobalSrcsStmt", mf.Stmts[0])
	}

	if !equalStrings(anyStrs(g.Sources), []string{"a.cpp", "b.cpp", "c.cpp"}) {
		t.Errorf("Sources = %v, want [a.cpp b.cpp c.cpp]", g.Sources)
	}
}

func TestParseInclude_RejectsSelfCycle(t *testing.T) {
	fs := newMemFS(map[string]string{"a.inc": "INCLUDE(a.inc)\n"})

	_, err := parseFile(fs, "a.inc")

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "INCLUDE cycle") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseInclude_RejectsTransitiveCycle(t *testing.T) {
	fs := newMemFS(map[string]string{
		"a.inc": "INCLUDE(b.inc)\n",
		"b.inc": "INCLUDE(a.inc)\n",
	})

	_, err := parseFile(fs, "a.inc")

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "INCLUDE cycle") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseIf_StringEquality(t *testing.T) {
	src := []byte(`IF (CXX_RT == "libcxxrt")
    SRCS(rt.c)
ENDIF()
`)

	mf, err := parse(testParserFS, "test.input", src)

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

	eq := condRoot(ifStmt.Cond)

	if eq.Kind != ckEq {
		t.Fatalf("IfStmt.Cond root = kind %d, want ckEq", eq.Kind)
	}

	if l := ifStmt.Cond[eq.L]; l.Kind != ckIdent || l.Name != "CXX_RT" {
		t.Errorf("Eq.L = %+v, want ident{CXX_RT}", l)
	}

	if r := ifStmt.Cond[eq.R]; r.Kind != ckString || r.Name != "libcxxrt" {
		t.Errorf("Eq.R = %+v, want string{libcxxrt}", r)
	}
}

func TestParseIf_NumericLessThan(t *testing.T) {
	src := []byte(`IF (ANDROID_API < 28)
    SRCS(android.c)
ENDIF()
`)

	mf, err := parse(testParserFS, "test.input", src)

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

	lt := condRoot(ifStmt.Cond)

	if lt.Kind != ckLt {
		t.Fatalf("IfStmt.Cond root = kind %d, want ckLt", lt.Kind)
	}

	if l := ifStmt.Cond[lt.L]; l.Kind != ckIdent || l.Name != "ANDROID_API" {
		t.Errorf("Lt.L = %+v, want ident{ANDROID_API}", l)
	}

	if r := ifStmt.Cond[lt.R]; r.Kind != ckInt || r.Ival != 28 {
		t.Errorf("Lt.R = %+v, want int{28}", r)
	}
}

func TestParseIf_NotEqualDesugars(t *testing.T) {
	src := []byte(`IF (OS_SDK != "ubuntu-20")
    SRCS(other_sdk.c)
ENDIF()
`)

	mf, err := parse(testParserFS, "test.input", src)

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	ifStmt, ok := mf.Stmts[0].(*IfStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *IfStmt", mf.Stmts[0])
	}

	not := condRoot(ifStmt.Cond)

	if not.Kind != ckNot {
		t.Fatalf("IfStmt.Cond root = kind %d, want ckNot (X != Y desugar)", not.Kind)
	}

	eq := ifStmt.Cond[not.L]

	if eq.Kind != ckEq {
		t.Fatalf("Not.Of = kind %d, want ckEq", eq.Kind)
	}

	if l := ifStmt.Cond[eq.L]; l.Kind != ckIdent || l.Name != "OS_SDK" {
		t.Errorf("Eq.L = %+v, want ident{OS_SDK}", l)
	}

	if r := ifStmt.Cond[eq.R]; r.Kind != ckString || r.Name != "ubuntu-20" {
		t.Errorf("Eq.R = %+v, want string{ubuntu-20}", r)
	}
}

func TestParseIf_ChainedComparisonRejected(t *testing.T) {
	_, err := parse(testParserFS, "test.input", []byte("IF (A == B == C)\nENDIF()\n"))

	if err == nil {
		t.Fatal("expected error for chained comparison, got nil")
	}

	if !strings.Contains(err.Error(), "chained comparison") {
		t.Errorf("error %q does not mention 'chained comparison'", err.Error())
	}
}

func TestParseIf_ComparisonInAndOr(t *testing.T) {
	src := []byte(`IF (SANITIZER_TYPE == undefined OR FUZZING)
    SRCS(x.c)
ENDIF()
`)

	mf, err := parse(testParserFS, "test.input", src)

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	ifStmt := mf.Stmts[0].(*IfStmt)

	or := condRoot(ifStmt.Cond)

	if or.Kind != ckOr {
		t.Fatalf("Cond root = kind %d, want ckOr (OR is outermost)", or.Kind)
	}

	if l := ifStmt.Cond[or.L]; l.Kind != ckEq {
		t.Errorf("OR.L = kind %d, want ckEq", l.Kind)
	}

	if r := ifStmt.Cond[or.R]; r.Kind != ckIdent || r.Name != "FUZZING" {
		t.Errorf("OR.R = %+v, want ident{FUZZING}", r)
	}
}

func TestParseIf_VersionLiteralStillWord(t *testing.T) {
	mf, err := parse(testParserFS, "test.input", []byte("VERSION(2025-06-20)\n"))

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

	if !equalStrings(anyStrs(u.Args), []string{"2025-06-20"}) {
		t.Errorf("VERSION args = %v, want [2025-06-20]", u.Args)
	}
}

func TestParseIf_PureIntInMacroArg(t *testing.T) {
	mf, err := parse(testParserFS, "test.input", []byte("IDE_FOLDER(42)\n"))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	u := mf.Stmts[0].(*UnknownStmt)

	if !equalStrings(anyStrs(u.Args), []string{"42"}) {
		t.Errorf("IDE_FOLDER args = %v, want [42]", u.Args)
	}
}

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

func TestParseYqlUdfModuleStmts(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantName string
		wantArgs []string
	}{
		{
			name:     "ydb",
			src:      "YQL_UDF_YDB(clickhouse_client_udf)\n",
			wantName: "YQL_UDF_YDB",
			wantArgs: []string{"clickhouse_client_udf"},
		},
		{
			name:     "contrib",
			src:      "YQL_UDF_CONTRIB(string_udf)\n",
			wantName: "YQL_UDF_CONTRIB",
			wantArgs: []string{"string_udf"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mf, err := parse(testParserFS, "test.input", []byte(tc.src))

			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			if len(mf.Stmts) != 1 {
				t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
			}

			m, ok := mf.Stmts[0].(*ModuleStmt)

			if !ok {
				t.Fatalf("Stmts[0] = %T, want *ModuleStmt", mf.Stmts[0])
			}

			if m.Name.string() != tc.wantName {
				t.Fatalf("ModuleStmt.Name = %q, want %q", m.Name.string(), tc.wantName)
			}

			if !equalStrings(anyStrs(m.Args), tc.wantArgs) {
				t.Fatalf("ModuleStmt.Args = %v, want %v", m.Args, tc.wantArgs)
			}
		})
	}
}

func TestParseSetAllowsEmptyValue(t *testing.T) {
	mf, err := parse(testParserFS, "test.input", []byte("SET(DISABLE_HYPERSCAN_BUILD)\n"))

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

	if s.Name != "DISABLE_HYPERSCAN_BUILD" {
		t.Fatalf("SET.Name = %q, want DISABLE_HYPERSCAN_BUILD", s.Name)
	}

	if s.Value != "" {
		t.Fatalf("SET.Value = %q, want empty string", s.Value)
	}
}

func TestParseIfAllowsElseAndEndifTags(t *testing.T) {
	src := []byte(`IF (OS_LINUX)
SRCS(a.cpp)
ELSE(OS_LINUX)
SRCS(b.cpp)
ENDIF(OS_LINUX)
`)
	mf, err := parse(testParserFS, "test.input", src)

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(mf.Stmts) != 1 {
		t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
	}

	if _, ok := mf.Stmts[0].(*IfStmt); !ok {
		t.Fatalf("Stmts[0] = %T, want *IfStmt", mf.Stmts[0])
	}
}

func TestParseRunPy3ProgramAsRunProgramStmt(t *testing.T) {
	src := []byte(`RUN_PY3_PROGRAM(
    tools/gen
    foo
    IN input.txt
    OUT output.txt
)
`)
	mf, err := parse(testParserFS, "test.input", src)

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(mf.Stmts) != 1 {
		t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
	}

	stmt, ok := mf.Stmts[0].(*RunProgramStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *RunProgramStmt", mf.Stmts[0])
	}

	if stmt.ToolPath.string() != "tools/gen" {
		t.Fatalf("ToolPath = %q, want tools/gen", stmt.ToolPath)
	}

	if !equalStrings(anyStrs(stmt.Args), []string{"foo"}) {
		t.Fatalf("Args = %v, want [foo]", stmt.Args)
	}

	if !equalStrings(anyStrs(stmt.INFiles), []string{"input.txt"}) {
		t.Fatalf("INFiles = %v, want [input.txt]", stmt.INFiles)
	}

	if !equalStrings(anyStrs(stmt.OUTFiles), []string{"output.txt"}) {
		t.Fatalf("OUTFiles = %v, want [output.txt]", stmt.OUTFiles)
	}
}

func TestParseRunProgramToolSection(t *testing.T) {
	src := []byte(`RUN_PROGRAM(
    tools/protoc
    --plugin=protoc-gen-cpp_styleguide=contrib/tools/protoc/plugins/cpp_styleguide
    foo.proto
    IN foo.proto
    TOOL contrib/tools/protoc/plugins/cpp_styleguide
    OUTPUT_INCLUDES contrib/libs/protobuf/src/google/protobuf/message.h
    OUT_NOAUTO foo.pb.h foo.pb.cc
)
`)
	mf, err := parse(testParserFS, "test.input", src)

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(mf.Stmts) != 1 {
		t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
	}

	stmt, ok := mf.Stmts[0].(*RunProgramStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *RunProgramStmt", mf.Stmts[0])
	}

	if !equalStrings(anyStrs(stmt.ToolPaths), []string{"contrib/tools/protoc/plugins/cpp_styleguide"}) {
		t.Fatalf("ToolPaths = %v, want [contrib/tools/protoc/plugins/cpp_styleguide]", stmt.ToolPaths)
	}

	if !equalStrings(anyStrs(stmt.OutputIncludes), []string{"contrib/libs/protobuf/src/google/protobuf/message.h"}) {
		t.Fatalf("OutputIncludes = %v, want [contrib/libs/protobuf/src/google/protobuf/message.h]", stmt.OutputIncludes)
	}
}

func setStmtByName(stmts []Stmt, name string) *SetStmt {
	for _, s := range stmts {
		if v, ok := s.(*SetStmt); ok && v.Name == name {
			return v
		}
	}

	return nil
}

func TestParseInclude_ExpandsVarFromEarlierSet(t *testing.T) {
	fs := newMemFS(map[string]string{
		"app/ya.make": "INCLUDE(cfg/name.inc)\n" +
			"INCLUDE(${ARCADIA_ROOT}/gen/artefacts_${CONFIG_NAME}_/peers.lst)\n" +
			"PY3_PROGRAM(app)\nPEERDIR(${FEATURE_PEERDIRS})\nEND()\n",
		"app/cfg/name.inc":                "SET(CONFIG_NAME caesar)\n",
		"gen/artefacts_caesar_/peers.lst": "SET(FEATURE_PEERDIRS feature/model)\n",
	})

	mf, err := parseFile(fs, "app/ya.make")

	if err != nil {
		t.Fatalf("parseFile failed: %v", err)
	}

	for _, s := range mf.Stmts {
		if _, isInc := s.(*IncludeStmt); isInc {
			t.Errorf("Stmts contains *IncludeStmt; expected inline expansion")
		}
	}

	fp := setStmtByName(mf.Stmts, "FEATURE_PEERDIRS")

	if fp == nil {
		t.Fatalf("variable-bearing INCLUDE was skipped: no SET(FEATURE_PEERDIRS); got %#v", mf.Stmts)
	}

	if fp.Value != "feature/model" {
		t.Errorf("FEATURE_PEERDIRS = %q, want feature/model", fp.Value)
	}

	var setIdx, modIdx = -1, -1

	for i, s := range mf.Stmts {
		switch v := s.(type) {
		case *SetStmt:
			if v.Name == "FEATURE_PEERDIRS" {
				setIdx = i
			}
		case *ModuleStmt:
			if modIdx == -1 {
				modIdx = i
			}
		}
	}

	if setIdx == -1 || modIdx == -1 || setIdx >= modIdx {
		t.Errorf("SET(FEATURE_PEERDIRS) at %d must precede PY3_PROGRAM at %d", setIdx, modIdx)
	}
}

func TestParseInclude_IgnoresArgumentsAfterFirst(t *testing.T) {
	fs := newMemFS(map[string]string{
		"ya.make":     "LIBRARY()\nINCLUDE(a.inc b_${NAME}.inc)\nEND()\n",
		"a.inc":       "SET(NAME value)\n",
		"b_value.inc": "SRCS(found.cpp)\n",
	})

	mf, err := parseFile(fs, "ya.make")

	if err != nil {
		t.Fatalf("parseFile failed: %v", err)
	}

	if setStmtByName(mf.Stmts, "NAME") == nil {
		t.Fatalf("first INCLUDE arg was not read; got %#v", mf.Stmts)
	}

	for _, s := range mf.Stmts {
		if v, ok := s.(*SrcsStmt); ok && equalStrings(anyStrs(v.Sources), []string{"found.cpp"}) {
			t.Fatalf("second INCLUDE arg was read; upstream ignores args after args[0]")
		}
	}
}

func TestParseInclude_MissingExpandedTargetIsSilent(t *testing.T) {
	fs := newMemFS(map[string]string{
		"ya.make": "LIBRARY()\nSET(NAME ghost)\nINCLUDE(${ARCADIA_ROOT}/missing_${NAME}.inc)\nSRCS(x.cpp)\nEND()\n",
	})

	mf, err := parseFile(fs, "ya.make")

	if err != nil {
		t.Fatalf("missing expanded include must not error: %v", err)
	}

	var srcs *SrcsStmt

	for _, s := range mf.Stmts {
		if v, ok := s.(*SrcsStmt); ok {
			srcs = v
		}
	}

	if srcs == nil || !equalStrings(anyStrs(srcs.Sources), []string{"x.cpp"}) {
		t.Errorf("unexpected stmts after missing include: %#v", mf.Stmts)
	}
}

func TestParseInclude_UnresolvedVarIsSilent(t *testing.T) {
	fs := newMemFS(map[string]string{
		"ya.make": "LIBRARY()\nINCLUDE(${UNSET}/x.inc)\nSRCS(x.cpp)\nEND()\n",
	})

	mf, err := parseFile(fs, "ya.make")

	if err != nil {
		t.Fatalf("unresolved include var must not error: %v", err)
	}

	if setStmtByName(mf.Stmts, "anything") != nil {
		t.Fatal("unexpected included content")
	}
}

func TestParseInclude_AbsoluteAfterExpansionRejected(t *testing.T) {
	fs := newMemFS(map[string]string{
		"ya.make": "LIBRARY()\nSET(ABS /tmp/nope.inc)\nINCLUDE(${ABS})\nEND()\n",
	})

	_, err := parseFile(fs, "ya.make")

	if err == nil {
		t.Fatal("expected absolute-path rejection, got nil")
	}

	if !strings.Contains(err.Error(), "absolute paths escape the source root") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseInclude_CycleWithExpandedKey(t *testing.T) {
	fs := newMemFS(map[string]string{
		"a.inc": "SET(DIR .)\nINCLUDE(${DIR}/a.inc)\n",
	})

	_, err := parseFile(fs, "a.inc")

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "INCLUDE cycle") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseInclude_SetInsideIfFeedsLaterInclude(t *testing.T) {
	fs := newMemFS(map[string]string{
		"ya.make": "LIBRARY()\nIF (OPENSOURCE)\nSET(NAME val)\nENDIF()\n" +
			"INCLUDE(${ARCADIA_ROOT}/gen_${NAME}.inc)\nEND()\n",
		"gen_val.inc": "SRCS(fromif.cpp)\n",
	})

	mf, err := parseFile(fs, "ya.make")

	if err != nil {
		t.Fatalf("parseFile failed: %v", err)
	}

	var srcs *SrcsStmt

	for _, s := range mf.Stmts {
		if v, ok := s.(*SrcsStmt); ok {
			srcs = v
		}
	}

	if srcs == nil || !equalStrings(anyStrs(srcs.Sources), []string{"fromif.cpp"}) {
		t.Fatalf("SET inside IF body did not feed later INCLUDE; got %#v", mf.Stmts)
	}
}

func TestParseInclude_ModdirDefaultIncludeSetListFeedsRunProgram(t *testing.T) {
	fs := newMemFS(map[string]string{
		"pkg/cfg/ya.make": "LIBRARY()\n" +
			"INCLUDE(${ARCADIA_ROOT}/pkg/codegen/run.inc)\n" +
			"END()\n",
		"pkg/codegen/run.inc": "IF (MODDIR MATCHES pkg/cfg)\n" +
			"    DEFAULT(CONFIG_PATH ${ARCADIA_ROOT}/${MODDIR})\n" +
			"ENDIF()\n" +
			"INCLUDE(${CONFIG_PATH}/modules.lst)\n" +
			"SET(GEN_ARGS ${MODULES_INCLUDED} --bindir ${BINDIR})\n" +
			"RUN_PROGRAM(pkg/cfg/tool ${GEN_ARGS} OUT out.cpp)\n",
		"pkg/cfg/modules.lst": "SET(MODULES_INCLUDED first.mod second.mod third.mod)\n",
	})

	mf, err := parseFile(fs, "pkg/cfg/ya.make")

	if err != nil {
		t.Fatalf("parseFile failed: %v", err)
	}

	if setStmtByName(mf.Stmts, "MODULES_INCLUDED") == nil {
		t.Fatalf("modules.lst was not included: no SET(MODULES_INCLUDED); got %#v", mf.Stmts)
	}

	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), &DeDuper{}, ModuleInstance{Path: source("pkg/cfg"), Kind: KindLib}, mf.Stmts,
		buildIfEnv(ModuleInstance{Path: source("pkg/cfg"), Kind: KindLib, Platform: testTargetP}), noWarn)

	if len(d.runPrograms) != 1 {
		t.Fatalf("runPrograms = %d, want 1", len(d.runPrograms))
	}

	got := anyStrs(d.runPrograms[0].Args)
	want := []string{"first.mod", "second.mod", "third.mod", "--bindir", "$(B)/pkg/cfg"}

	if !equalStrings(got, want) {
		t.Fatalf("RUN_PROGRAM args = %v, want %v (the SET-list must expand, not survive as ${MODULES_INCLUDED})", got, want)
	}
}

func condRoot(c []CondNode) CondNode {
	return c[len(c)-1]
}
