package main

import "testing"

// TestStarlark_RunPython3 pins the run_python3() builtin: the script head plus the
// keyword sections produce a RunPythonStmt identical to the parsed RUN_PYTHON3.
func TestStarlark_RunPython3(t *testing.T) {
	env := DefaultIfEnv.clone()

	assertSameStmts(t,
		evalStarStr(t, `library(srcs = ["a.cpp"] + run_python3(
    "gen.py",
    args = ["--out", "x"],
    ins = ["in.txt"],
    outs = ["out.cpp"],
    out_noauto = ["log"],
    stdout = ["s.out"],
    env = ["K=V"],
    output_includes = ["h.h"],
    cwd = "sub",
))`, env),
		parseMakeStr(t, "LIBRARY()\nSRCS(a.cpp)\n"+
			"RUN_PYTHON3(gen.py --out x IN in.txt OUT out.cpp OUT_NOAUTO log "+
			"STDOUT s.out ENV K=V OUTPUT_INCLUDES h.h CWD sub)\nEND()\n"))
}
