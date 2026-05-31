package main

import (
	"reflect"
	"sort"
	"testing"
)

// scriptFixtureFS builds a memFS whose build/scripts/ holds Python scripts that
// exercise real imports plus the three false-positive traps that a naive textual
// match would wrongly treat as dependencies.
func scriptFixtureFS() *memFS {
	return newMemFS(map[string]string{
		"build/scripts/link_exe.py": "import os\n" +
			"import process_command_files as pcf\n" +
			"import thinlto_cache\n" +
			"from process_whole_archive_option import ProcessWholeArchiveOption\n" +
			"def f(args):\n" +
			"    parser.add_option('--objcopy-exe')\n" + // 'objcopy' must NOT pull objcopy.py
			"    plugins = list(sorted(args))\n" + //       'list' must NOT pull list.py
			"    thinlto_cache.preprocess(opts)\n", //      'preprocess' must NOT pull preprocess.py
		"build/scripts/process_whole_archive_option.py": "import process_command_files as pcf\n" +
			"# an unknown option error may occur here\n", // 'error' in a comment must NOT pull error.py
		"build/scripts/fs_tools.py":              "import shutil\nimport process_command_files as pcf\n",
		"build/scripts/process_command_files.py": "import sys\n",
		"build/scripts/thinlto_cache.py":         "import os\n",
		"build/scripts/objcopy.py":               "import sys\n",
		"build/scripts/list.py":                  "import sys\n",
		"build/scripts/preprocess.py":            "import sys\n",
		"build/scripts/error.py":                 "import sys\n",
		// vcs_info uses a local variable named `wrapper`; must NOT pull wrapper.py.
		"build/scripts/vcs_info.py": "import textwrap\n" +
			"wrapper = textwrap.TextWrapper()\n" +
			"out = wrapper.wrap(text)\n",
		"build/scripts/wrapper.py": "import sys\n",
		// gen_py3_reg prints gen_py_reg.py in a usage string; must NOT pull it.
		"build/scripts/gen_py3_reg.py": "import sys\n" +
			"print('Usage: <path/to/gen_py_reg.py> <module> <out>')\n",
		"build/scripts/gen_py_reg.py": "import sys\n",
	})
}

func TestBuildScriptDepClosure_ImportsOnly_NoFalsePositives(t *testing.T) {
	closure := buildScriptDepClosure(scriptFixtureFS())

	cases := map[string][]string{
		"build/scripts/link_exe.py": {
			"build/scripts/process_command_files.py",
			"build/scripts/process_whole_archive_option.py",
			"build/scripts/thinlto_cache.py",
		},
		"build/scripts/fs_tools.py":                     {"build/scripts/process_command_files.py"},
		"build/scripts/process_whole_archive_option.py": {"build/scripts/process_command_files.py"},
		"build/scripts/process_command_files.py":        {},
		// False-positive traps: a local var, a comment word, a usage string.
		"build/scripts/vcs_info.py":    {},
		"build/scripts/gen_py3_reg.py": {},
	}

	for script, want := range cases {
		got := closure[script]
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		sort.Strings(got)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("closure[%s]:\n  got  %v\n  want %v", script, got, want)
		}
	}
}

func emitOneNodeInputs(e Emitter, closure scriptDepClosure) []string {
	switch t := e.(type) {
	case *BufferedEmitter:
		t.scriptClosure = closure
	case *StreamingEmitter:
		t.scriptClosure = closure
	}
	n := &Node{
		Cmds:             []Cmd{{CmdArgs: []string{"link"}, Env: map[string]string{}}},
		Env:              map[string]string{},
		Inputs:           []VFS{Source("build/scripts/link_exe.py"), Build("a/foo.o")},
		KV:               map[string]interface{}{"p": "LD"},
		Outputs:          []VFS{Build("a/prog")},
		Platform:         "linux",
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
	}
	e.Emit(n)
	rels := make([]string, 0, len(n.Inputs))
	for _, in := range n.Inputs {
		rels = append(rels, in.String())
	}
	sort.Strings(rels)
	return rels
}

// TestScriptClosureExpansion_StreamingMatchesBuffered guards the invariant that
// matters for the executor: the script-closure expansion happens per-node in
// Emit, so the streaming build path (StreamingEmitter -> executor) and the -G dump
// path (BufferedEmitter) produce identical node inputs. A regression that moved
// the expansion back into a buffered-only post-pass would diverge here.
func TestScriptClosureExpansion_StreamingMatchesBuffered(t *testing.T) {
	closure := buildScriptDepClosure(scriptFixtureFS())

	buffered := emitOneNodeInputs(NewBufferedEmitter(), closure)
	streaming := emitOneNodeInputs(NewStreamingEmitter(func(*Node) {}), closure)

	if !reflect.DeepEqual(buffered, streaming) {
		t.Fatalf("streaming vs buffered node inputs diverge:\n  buffered  %v\n  streaming %v", buffered, streaming)
	}

	// Expansion must actually have happened: the wrapper's helper closure is present.
	want := []string{
		"$(S)/build/scripts/process_command_files.py",
		"$(S)/build/scripts/process_whole_archive_option.py",
		"$(S)/build/scripts/thinlto_cache.py",
	}
	for _, w := range want {
		found := false
		for _, in := range buffered {
			if in == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expanded inputs missing %s; got %v", w, buffered)
		}
	}
}
