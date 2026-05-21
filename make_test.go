package main

import "testing"

// TestParseMakeFlags_TttSandboxing reproduces ticket 2: ydb invokes
// `ay make ... -ttt --sandboxing util/ut ...`. Before the fix, the unregistered
// `-t` / `--sandboxing` options make getopt return ErrUnknownOpt, which
// parseMakeFlags re-throws ("getopt: unrecognized option"). After the fix the
// invocation parses, with -ttt captured as testLevel=3 and --sandboxing as a bool.
func TestParseMakeFlags_TttSandboxing(t *testing.T) {
	var mf *makeFlags

	exc := Try(func() {
		mf = parseMakeFlags([]string{
			"-G", "-j", "0", "-ttt", "--sandboxing",
			"-DOS_SDK=local", "--host-platform-flag=OS_SDK=local",
			"--source-root", "/home/pg/monorepo/ydb", "util/ut",
		})
	})

	if exc != nil {
		t.Fatalf("parseMakeFlags threw: %s", exc.Error())
	}

	if mf.testLevel != 3 {
		t.Errorf("testLevel = %d, want 3", mf.testLevel)
	}

	if !mf.sandboxing {
		t.Errorf("sandboxing = false, want true")
	}

	if !mf.dumpGraph {
		t.Errorf("dumpGraph = false, want true (-G)")
	}

	if mf.threads != 0 {
		t.Errorf("threads = %d, want 0 (-j 0)", mf.threads)
	}

	if mf.srcRoot != "/home/pg/monorepo/ydb" {
		t.Errorf("srcRoot = %q, want /home/pg/monorepo/ydb", mf.srcRoot)
	}

	if len(mf.targets) != 1 || mf.targets[0] != "util/ut" {
		t.Errorf("targets = %v, want [util/ut]", mf.targets)
	}

	if mf.tflags["OS_SDK"] != "local" {
		t.Errorf("tflags[OS_SDK] = %q, want local", mf.tflags["OS_SDK"])
	}

	if mf.hflags["OS_SDK"] != "local" {
		t.Errorf("hflags[OS_SDK] = %q, want local", mf.hflags["OS_SDK"])
	}
}

// TestParseMakeFlags_TestLevelCounts pins the -t clustering semantics so a
// later change can't silently collapse the level to a bool.
func TestParseMakeFlags_TestLevelCounts(t *testing.T) {
	cases := []struct {
		flag string
		want int
	}{
		{"-t", 1},
		{"-tt", 2},
		{"-ttt", 3},
	}

	for _, tc := range cases {
		var mf *makeFlags

		exc := Try(func() {
			mf = parseMakeFlags([]string{tc.flag, "--source-root", "/home/pg/monorepo/ydb", "util/ut"})
		})
		if exc != nil {
			t.Fatalf("%s: parseMakeFlags threw: %s", tc.flag, exc.Error())
		}

		if mf.testLevel != tc.want {
			t.Errorf("%s: testLevel = %d, want %d", tc.flag, mf.testLevel, tc.want)
		}
	}
}
