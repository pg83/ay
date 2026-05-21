package main

import "testing"

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
			mf, err := Parse(testParserFS, "test.input", []byte(tc.src))
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
			if m.Name != tc.wantName {
				t.Fatalf("ModuleStmt.Name = %q, want %q", m.Name, tc.wantName)
			}
			if !equalStrings(m.Args, tc.wantArgs) {
				t.Fatalf("ModuleStmt.Args = %v, want %v", m.Args, tc.wantArgs)
			}
		})
	}
}

func TestParseSetAllowsEmptyValue(t *testing.T) {
	mf, err := Parse(testParserFS, "test.input", []byte("SET(DISABLE_HYPERSCAN_BUILD)\n"))
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
	mf, err := Parse(testParserFS, "test.input", src)
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
	mf, err := Parse(testParserFS, "test.input", src)
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
	if stmt.ToolPath != "tools/gen" {
		t.Fatalf("ToolPath = %q, want tools/gen", stmt.ToolPath)
	}
	if !equalStrings(stmt.Args, []string{"foo"}) {
		t.Fatalf("Args = %v, want [foo]", stmt.Args)
	}
	if !equalStrings(stmt.INFiles, []string{"input.txt"}) {
		t.Fatalf("INFiles = %v, want [input.txt]", stmt.INFiles)
	}
	if !equalStrings(stmt.OUTFiles, []string{"output.txt"}) {
		t.Fatalf("OUTFiles = %v, want [output.txt]", stmt.OUTFiles)
	}
}
