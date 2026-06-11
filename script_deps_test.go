package main

import (
	"reflect"
	"testing"
)

// scriptFixtureFS builds a memFS whose build/scripts/ holds Python scripts that
// exercise real imports plus the three false-positive traps that a naive textual
// match would wrongly treat as dependencies.
func scriptFixtureFS() *MemFS {
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

func TestBuildScriptTable_ImportsOnly_NoFalsePositives(t *testing.T) {
	table := buildScriptTable(scriptFixtureFS())

	// Each entry is [self, …sorted transitive closure]. Verify against the rel paths.
	cases := map[string][]string{
		"build/scripts/link_exe.py": {
			"build/scripts/link_exe.py", // self first
			"build/scripts/process_command_files.py",
			"build/scripts/process_whole_archive_option.py",
			"build/scripts/thinlto_cache.py",
		},
		"build/scripts/fs_tools.py": {
			"build/scripts/fs_tools.py",
			"build/scripts/process_command_files.py",
		},
		"build/scripts/process_command_files.py": {"build/scripts/process_command_files.py"},
		// False-positive traps: a local var, a comment word, a usage string — no closure.
		"build/scripts/vcs_info.py":    {"build/scripts/vcs_info.py"},
		"build/scripts/gen_py3_reg.py": {"build/scripts/gen_py3_reg.py"},
	}

	for script, want := range cases {
		got := table[Source(script)]
		if len(got) == 0 {
			t.Errorf("table[%s] is empty (must contain at least self)", script)
			continue
		}
		if got[0] != Source(script) {
			t.Errorf("table[%s][0] = %s, want self", script, got[0].string())
		}
		gotRel := make([]string, len(got))
		for i, v := range got {
			gotRel[i] = v.rel()
		}
		// closure (got[1:]) is sorted; self is first. Compare full slice.
		if !reflect.DeepEqual(gotRel, want) {
			t.Errorf("table[%s]:\n  got  %v\n  want %v", script, gotRel, want)
		}
	}
}
