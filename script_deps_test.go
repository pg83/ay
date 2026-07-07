package main

import (
	"reflect"
	"testing"
)

func scriptFixtureFS() *MemFS {
	return newMemFS(map[string]string{
		"build/scripts/link_exe.py": "import os\n" +
			"import process_command_files as pcf\n" +
			"import thinlto_cache\n" +
			"from process_whole_archive_option import ProcessWholeArchiveOption\n" +
			"def f(args):\n" +
			"    parser.add_option('--objcopy-exe')\n" +
			"    plugins = list(sorted(args))\n" +
			"    thinlto_cache.preprocess(opts)\n",
		"build/scripts/process_whole_archive_option.py": "import process_command_files as pcf\n" +
			"# an unknown option error may occur here\n",
		"build/scripts/fs_tools.py":              "import shutil\nimport process_command_files as pcf\n",
		"build/scripts/process_command_files.py": "import sys\n",
		"build/scripts/thinlto_cache.py":         "import os\n",
		"build/scripts/objcopy.py":               "import sys\n",
		"build/scripts/list.py":                  "import sys\n",
		"build/scripts/preprocess.py":            "import sys\n",
		"build/scripts/error.py":                 "import sys\n",

		"build/scripts/vcs_info.py": "import textwrap\n" +
			"wrapper = textwrap.TextWrapper()\n" +
			"out = wrapper.wrap(text)\n",
		"build/scripts/wrapper.py": "import sys\n",

		"build/scripts/gen_py3_reg.py": "import sys\n" +
			"print('Usage: <path/to/gen_py_reg.py> <module> <out>')\n",
		"build/scripts/gen_py_reg.py": "import sys\n",
	})
}

func TestBuildScriptTable_ImportsOnly_NoFalsePositives(t *testing.T) {
	table := buildScriptTable(scriptFixtureFS())

	cases := map[string][]string{
		"build/scripts/link_exe.py": {
			"build/scripts/link_exe.py",
			"build/scripts/process_command_files.py",
			"build/scripts/process_whole_archive_option.py",
			"build/scripts/thinlto_cache.py",
		},
		"build/scripts/fs_tools.py": {
			"build/scripts/fs_tools.py",
			"build/scripts/process_command_files.py",
		},
		"build/scripts/process_command_files.py": {"build/scripts/process_command_files.py"},

		"build/scripts/vcs_info.py":    {"build/scripts/vcs_info.py"},
		"build/scripts/gen_py3_reg.py": {"build/scripts/gen_py3_reg.py"},
	}

	for script, want := range cases {
		got := table[source(script)]

		if len(got) == 0 {
			t.Errorf("table[%s] is empty (must contain at least self)", script)

			continue
		}

		if got[0] != source(script) {
			t.Errorf("table[%s][0] = %s, want self", script, got[0].string())
		}

		gotRel := make([]string, len(got))

		for i, v := range got {
			gotRel[i] = v.relString()
		}

		if !reflect.DeepEqual(gotRel, want) {
			t.Errorf("table[%s]:\n  got  %v\n  want %v", script, gotRel, want)
		}
	}
}
