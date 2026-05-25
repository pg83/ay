package main

import (
	"reflect"
	"testing"
)

func TestComposeDynLibInputs_IncludesVcsAndHelperScripts(t *testing.T) {
	got := composeDynLibInputs(
		[]VFS{
			Intern("$(B)/contrib/libs/libiconv/static/liblibs-libiconv-static.a"),
			Intern("$(B)/build/cow/on/libbuild-cow-on.a"),
		},
		[]VFS{
			Intern("$(B)/contrib/libs/musl/include/musl.py.pyplugin"),
		},
		Intern("$(B)/tools/fix_elf/fix_elf"),
		"contrib/libs/libiconv/dynamic",
		"libiconv.exports",
	)

	want := []string{
		// BUILD_ROOT block in first-occurrence (input) order, not sorted —
		// node-input order is normalized away by the gate.
		"$(B)/contrib/libs/libiconv/static/liblibs-libiconv-static.a",
		"$(B)/build/cow/on/libbuild-cow-on.a",
		"$(B)/contrib/libs/musl/include/musl.py.pyplugin",
		"$(B)/tools/fix_elf/fix_elf",
		"$(S)/build/scripts/vcs_info.py",
		"$(S)/build/scripts/c_templates/svn_interface.c",
		"$(S)/build/scripts/link_dyn_lib.py",
		"$(S)/build/scripts/link_exe.py",
		"$(S)/contrib/libs/libiconv/dynamic/libiconv.exports",
		"$(S)/build/scripts/thinlto_cache.py",
		"$(S)/build/scripts/process_command_files.py",
		"$(S)/build/scripts/process_whole_archive_option.py",
		"$(S)/build/scripts/fs_tools.py",
		"$(S)/build/scripts/c_templates/svnversion.h",
	}

	if !reflect.DeepEqual(vfsStrings(got), want) {
		t.Fatalf("composeDynLibInputs() = %#v, want %#v", vfsStrings(got), want)
	}
}
