package main

import "testing"

func TestGen_GperfScansSiblingGeneratedHeader(t *testing.T) {
	files := map[string]string{}
	writeBisonProducer(files)
	writeToolProgram(files, "contrib/tools/gperf", "gperf")

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(
    tags.gperf
    parser.y
)
END()
`)
	writeTestModuleFile(files, "mod/tags.gperf", `%{
#include "parser.h"
%}
%%
`)
	writeTestModuleFile(files, "mod/parser.y", "%%\n")

	_, warns := testGenWarns(newMemFS(files), "mod")

	assertNoMissingInclude(t, warns, "parser.h")
}
