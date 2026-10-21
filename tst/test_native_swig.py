import posixpath
import unittest

import lib


SWIG_LIB = "contrib/tools/swig/Lib"


def perl_tree(module, source="../api.swg"):
    source_path = posixpath.normpath("bindings/perl/" + source)
    source_dir = posixpath.dirname(source_path)
    files = {
        "bindings/perl/ya.make": (
            f"{module}\n{lib.BARE_MODULE}SET(SWIG_LANG perl)\n"
            f"SRCS({source})\nEND()\n"
        ),
        source_path: '%include "api.i"\n%header %{\n#include "bindings/api.h"\n%}\n',
        f"{source_dir}/api.i": '%include "detail.i"\n',
        f"{source_dir}/detail.i": "",
        "bindings/api.h": '#include "detail.h"\n',
        "bindings/detail.h": "",
        f"{SWIG_LIB}/perl5/ya.make": (
            f"LIBRARY()\n{lib.BARE_MODULE}"
            f"ADDINCL(GLOBAL FOR swig {SWIG_LIB}/perl5 GLOBAL FOR swig {SWIG_LIB})\nEND()\n"
        ),
        "build/platform/perl/ya.make": (
            f"LIBRARY()\n{lib.BARE_MODULE}CFLAGS(GLOBAL -DPERL_SDK)\nEND()\n"
        ),
    }
    for name in ("swig.swg", "go.swg", "java.swg", "perl5.swg", "python.swg"):
        files[f"{SWIG_LIB}/{name}"] = ""
    lib.tool_program(files, "contrib/tools/swig", "swig")
    return files


class NativeSwigTest(unittest.TestCase):
    def test_perl_wrapper_and_module_outputs(self):
        for module, output, suffix in (
            ("DLL(Binding PREFIX auto/Binding/lib)", "auto/Binding/libBinding.so", ".pic.o"),
            ("LIBRARY(Binding)", "libBinding.a", ".o"),
        ):
            with self.subTest(module=module):
                graph = lib.make(perl_tree(module), "bindings/perl")
                wrapper = "$(B)/bindings/perl/__/api_wrap.swg.cpp"
                perl = "$(B)/bindings/perl/Binding.pm"
                generated = lib.node_by_output(graph, wrapper)
                self.assertEqual(generated["outputs"], [wrapper, perl])
                self.assertEqual(generated["env"], {"ARCADIA_ROOT_DISTBUILD": "$(S)"})
                self.assertEqual(generated["cmds"][0]["cmd_args"], [
                    "$(B)/contrib/tools/swig/swig", "-c++", "-cpperraswarn",
                    "-module", "Binding", "-shadow", "-perl", "-o", wrapper,
                    "-outdir", "$(B)/bindings/perl", "-I$(B)", "-I$(S)",
                    f"-I$(S)/{SWIG_LIB}/perl5", f"-I$(S)/{SWIG_LIB}",
                    "$(S)/bindings/api.swg",
                ])
                for source in ("api.i", "detail.i"):
                    self.assertIn(f"$(S)/bindings/{source}", generated["inputs"])
                self.assertNotIn("$(S)/bindings/api.h", generated["inputs"])
                compiled = lib.node_by_output(graph, wrapper + suffix)
                self.assertIn("-DPERL_SDK", compiled["cmds"][0]["cmd_args"])
                for source in ("api.h", "detail.h"):
                    self.assertIn(f"$(S)/bindings/{source}", compiled["inputs"])
                self.assertIn(generated["uid"], compiled["deps"])
                linked = lib.node_by_output(graph, "$(B)/bindings/perl/" + output)
                self.assertEqual(linked["outputs"], ["$(B)/bindings/perl/" + output, perl])

    def test_wrapper_build_directory_tracks_resolved_source_directory(self):
        for source, relative in (
            ("api.swg", "api"),
            ("sub/api.swg", "_/sub/api"),
            ("../api.swg", "__/api"),
            ("../../shared/api.swg", "__/__/shared/api"),
        ):
            with self.subTest(source=source):
                graph = lib.make(perl_tree("LIBRARY(Binding)", source), "bindings/perl")
                wrapper = f"$(B)/bindings/perl/{relative}_wrap.swg.cpp"
                self.assertEqual(lib.node_by_output(graph, wrapper)["outputs"][0], wrapper)

    def test_perl_module_output_belongs_to_its_producing_library(self):
        files = perl_tree("LIBRARY(Binding)")
        files["app/ya.make"] = f"PROGRAM(app)\n{lib.BARE_MODULE}PEERDIR(bindings/perl)\nEND()\n"
        graph = lib.make(files, "app")
        perl = "$(B)/bindings/perl/Binding.pm"
        self.assertIn(perl, lib.node_by_output(graph, "$(B)/bindings/perl/libBinding.a")["outputs"])
        self.assertNotIn(perl, lib.node_by_output(graph, "$(B)/app/app")["outputs"])


if __name__ == "__main__":
    unittest.main()
