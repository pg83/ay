package main

import (
	"reflect"
	"slices"
	"sort"
	"testing"
)

func TestComposeDynLibInputs_IncludesVcsAndHelperScripts(t *testing.T) {
	scr := func(rel string) VFS { return intern("$(S)/build/scripts/" + rel) }
	// Import closures as the gen-time table would have them.
	scripts := ScriptDeps{
		scr("vcs_info.py"): {scr("vcs_info.py")},
		scr("link_dyn_lib.py"): {
			scr("link_dyn_lib.py"), scr("link_exe.py"), scr("process_command_files.py"),
			scr("process_whole_archive_option.py"), scr("thinlto_cache.py"),
		},
		scr("fs_tools.py"): {scr("fs_tools.py"), scr("process_command_files.py")},
	}

	got := composeDynLibInputs(
		newNodeArenas(),
		[]VFS{
			intern("$(B)/contrib/libs/libiconv/static/liblibs-libiconv-static.a"),
			intern("$(B)/build/cow/on/libbuild-cow-on.a"),
		},
		[]VFS{
			intern("$(B)/contrib/libs/foolib/include/foolib.py.pyplugin"),
		},
		intern("$(B)/tools/fix_elf/fix_elf"),
		"contrib/libs/libiconv/dynamic",
		"libiconv.exports",
		scripts,
	)

	// composeDynLibInputs may list a shared helper more than once; normalization
	// dedups, so compare as a set.
	want := []string{
		"$(B)/build/cow/on/libbuild-cow-on.a",
		"$(B)/contrib/libs/foolib/include/foolib.py.pyplugin",
		"$(B)/contrib/libs/libiconv/static/liblibs-libiconv-static.a",
		"$(B)/tools/fix_elf/fix_elf",
		"$(S)/build/scripts/c_templates/svn_interface.c",
		"$(S)/build/scripts/c_templates/svnversion.h",
		"$(S)/build/scripts/fs_tools.py",
		"$(S)/build/scripts/link_dyn_lib.py",
		"$(S)/build/scripts/link_exe.py",
		"$(S)/build/scripts/process_command_files.py",
		"$(S)/build/scripts/process_whole_archive_option.py",
		"$(S)/build/scripts/thinlto_cache.py",
		"$(S)/build/scripts/vcs_info.py",
		"$(S)/contrib/libs/libiconv/dynamic/libiconv.exports",
	}

	got2 := vfsStrings(got.flat())
	sort.Strings(got2)
	got2 = slices.Compact(got2)
	if !reflect.DeepEqual(got2, want) {
		t.Fatalf("composeDynLibInputs() set = %#v, want %#v", got2, want)
	}
}
