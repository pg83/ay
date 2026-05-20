package main

import (
	"reflect"
	"testing"
)

func TestComposeDynLibInputs_IncludesVcsAndHelperScripts(t *testing.T) {
	got := composeDynLibInputs(
		[]VFS{
			Build("contrib/libs/libiconv/static/liblibs-libiconv-static.a"),
			Build("build/cow/on/libbuild-cow-on.a"),
		},
		[]VFS{
			Build("contrib/libs/musl/include/musl.py.pyplugin"),
		},
		Build("tools/fix_elf/fix_elf"),
		"contrib/libs/libiconv/dynamic",
		"libiconv.exports",
	)

	want := []string{
		"$(B)/build/cow/on/libbuild-cow-on.a",
		"$(B)/contrib/libs/libiconv/static/liblibs-libiconv-static.a",
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
