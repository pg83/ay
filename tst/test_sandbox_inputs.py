import unittest

import lib
from test_proto_cpp import PB_RUNTIME, WRAPPER, proto_files


LIBRARY = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


class SandboxInputsTest(unittest.TestCase):
    def test_generated_chain_keeps_inputs_on_their_producer(self):
        files = {
            "mod/ya.make": (
                LIBRARY
                + "ADDINCL(${ARCADIA_BUILD_ROOT}/mod)\n"
                + "COPY_FILE(seed.txt first.h OUTPUT_INCLUDES mod/public.h)\n"
                + "COPY_FILE(${BINDIR}/first.h second.h)\n"
                + "SRCS(main.cpp sibling.cpp)\nEND()\n"
            ),
            "mod/seed.txt": "generator input\n",
            "mod/public.h": '#include "nested.h"\n',
            "mod/nested.h": "",
            "mod/main.cpp": '#include "second.h"\n',
            "mod/sibling.cpp": '#include "sibling.h"\n',
            "mod/sibling.h": "",
            "build/scripts/fs_tools.py": "import copy_helper\n",
            "build/scripts/copy_helper.py": "",
        }
        for args in ((), ("--dist",)):
            with self.subTest(args=args):
                graph = lib.make(files, "mod", *args)
                first = lib.node_by_output(graph, "$(B)/mod/first.h")
                second = lib.node_by_output(graph, "$(B)/mod/second.h")
                compile_node = lib.node_by_output(graph, "$(B)/mod/main.cpp.o")
                self.assertEqual(set(first["inputs"]), {
                    "$(S)/mod/seed.txt", "$(S)/build/scripts/fs_tools.py",
                    "$(S)/build/scripts/copy_helper.py",
                })
                self.assertEqual(set(second["inputs"]), {
                    "$(B)/mod/first.h", "$(S)/mod/public.h", "$(S)/mod/nested.h",
                    "$(S)/build/scripts/fs_tools.py", "$(S)/build/scripts/copy_helper.py",
                })
                self.assertEqual(set(compile_node["inputs"]), {
                    "$(S)/mod/main.cpp", "$(B)/mod/second.h",
                })
                self.assertIn(first["uid"], second["deps"])
                self.assertIn(second["uid"], compile_node["deps"])
                sibling = lib.node_by_output(graph, "$(B)/mod/sibling.cpp.o")
                self.assertEqual(set(sibling["inputs"]), {
                    "$(S)/mod/sibling.cpp", "$(S)/mod/sibling.h",
                })

    def test_template_forwards_include_closure_and_generated_dependencies(self):
        files = {
            "mod/ya.make": (
                LIBRARY
                + "ADDINCL(${ARCADIA_BUILD_ROOT}/mod)\n"
                + "COPY_FILE(seed.txt generated.h OUTPUT_INCLUDES mod/public.h)\n"
                + "SRCS(main.cpp.in)\nEND()\n"
            ),
            "mod/main.cpp.in": '#include "middle.h"\n',
            "mod/middle.h": '#include "generated.h"\n',
            "mod/public.h": '#include "middle.h"\n',
            "mod/seed.txt": "",
            "build/scripts/configure_file.py": "",
            "build/scripts/fs_tools.py": "",
        }
        for args in ((), ("--dist",)):
            with self.subTest(args=args):
                graph = lib.make(files, "mod", *args)
                generated = lib.node_by_output(graph, "$(B)/mod/generated.h")
                configured = lib.node_by_output(graph, "$(B)/mod/main.cpp")
                compile_node = lib.node_by_output(graph, "$(B)/mod/main.cpp.o")
                self.assertEqual(set(compile_node["inputs"]), {
                    "$(B)/mod/main.cpp", "$(S)/mod/main.cpp.in",
                    "$(S)/mod/middle.h", "$(B)/mod/generated.h", "$(S)/mod/public.h",
                })
                self.assertIn(generated["uid"], configured["deps"])
                self.assertIn(generated["uid"], compile_node["deps"])
                self.assertIn(configured["uid"], compile_node["deps"])

    def test_text_copy_does_not_follow_source_includes(self):
        files = {
            "mod/ya.make": (
                LIBRARY + "ADDINCL(${ARCADIA_BUILD_ROOT}/mod)\n"
                "COPY_FILE(seed.txt generated.h)\nCOPY_FILE(src.h copied.h)\nSRCS(use.cpp)\nEND()\n"
            ),
            "mod/seed.txt": "",
            "mod/src.h": '#include "generated.h"\n',
            "mod/use.cpp": '#include "copied.h"\n',
        }
        graph = lib.make(files, "mod", "--dist")
        copy = lib.node_by_output(graph, "$(B)/mod/copied.h")
        compiled = lib.node_by_output(graph, "$(B)/mod/use.cpp.o")
        self.assertEqual(set(copy["inputs"]), {"$(S)/mod/src.h"})
        self.assertEqual(set(compiled["inputs"]), {"$(S)/mod/use.cpp", "$(B)/mod/copied.h"})
        self.assertFalse(any("$(B)/mod/generated.h" in n["outputs"] for n in graph["graph"]))
        self.assertIn(copy["uid"], compiled["deps"])

    def test_secondary_opaque_input_keeps_main_output(self):
        files = {
            "mod/ya.make": (LIBRARY + "RUN_PROGRAM(tool OUT_NOAUTO first.bin second.pyc)\n"
                            "ARCHIVE(NAME data.inc second.pyc)\nSRCS(use.cpp)\nEND()\n"),
            "mod/use.cpp": '#include "data.inc"\n',
        }
        lib.tool_program(files, "tool", "tool")
        lib.tool_program(files, "tools/archiver", "archiver")
        graph = lib.make(files, "mod", "--sandboxing")
        archived = lib.node_by_output(graph, "$(B)/mod/data.inc")
        self.assertCountEqual(archived["inputs"], [
            "$(B)/mod/second.pyc", "$(B)/mod/first.bin", "$(B)/tools/archiver/archiver",
        ])
        producer = lib.node_by_output(graph, "$(B)/mod/second.pyc")
        self.assertIn(producer["uid"], archived["deps"])
        self.assertNotIn("$(B)/mod/first.bin", archived["cmds"][0]["cmd_args"])

    def test_runner_outputs_only_forward_declared_includes(self):
        for runner in ("RUN_PROGRAM(tool IN seed.txt OUT generated.cpp OUTPUT_INCLUDES mod/public.h)",
                       "RUN_PYTHON3(gen.py IN seed.txt OUT generated.cpp OUTPUT_INCLUDES mod/public.h)"):
            with self.subTest(runner=runner):
                files = {
                    "mod/ya.make": LIBRARY + runner + "\nEND()\n",
                    "mod/seed.txt": "",
                    "mod/gen.py": "",
                    "mod/public.h": '#include "nested.h"\n',
                    "mod/nested.h": "",
                }
                lib.tool_program(files, "tool", "tool")
                graph = lib.make(files, "mod", "--dist")
                generated = lib.node_by_output(graph, "$(B)/mod/generated.cpp")
                compiled = lib.node_by_output(graph, "$(B)/mod/generated.cpp.o")
                self.assertIn("$(S)/mod/seed.txt", generated["inputs"])
                self.assertNotIn("$(S)/mod/public.h", generated["inputs"])
                self.assertEqual(set(compiled["inputs"]), {
                    "$(B)/mod/generated.cpp", "$(S)/mod/public.h", "$(S)/mod/nested.h",
                })
                self.assertIn(generated["uid"], compiled["deps"])

    def test_template_forwarding_applies_to_an_arbitrary_generator(self):
        files = {
            "mod/ya.make": (
                LIBRARY + "ADDINCL(${ARCADIA_BUILD_ROOT}/mod)\n"
                "COPY_FILE(seed.txt generated.h)\nRUN_PYTHON3(gen.py IN main.cpp.in OUT generated.cpp)\nEND()\n"
            ),
            "mod/gen.py": "",
            "mod/main.cpp.in": '#include "middle.h"\n',
            "mod/middle.h": '#include "generated.h"\n',
        }
        files["mod/seed.txt"] = ""
        graph = lib.make(files, "mod", "--dist")
        producer = lib.node_by_output(graph, "$(B)/mod/generated.cpp")
        compiled = lib.node_by_output(graph, "$(B)/mod/generated.cpp.o")
        header = lib.node_by_output(graph, "$(B)/mod/generated.h")
        self.assertEqual(set(compiled["inputs"]), {
            "$(B)/mod/generated.cpp", "$(S)/mod/main.cpp.in", "$(S)/mod/middle.h", "$(B)/mod/generated.h",
        })
        self.assertIn(header["uid"], compiled["deps"])
        self.assertIn(header["uid"], producer["deps"])
        self.assertIn(producer["uid"], compiled["deps"])

    def test_template_forwarding_survives_registration_after_emission(self):
        files = {
            "mod/ya.make": LIBRARY + "ARCHIVE(NAME data.inc template.cpp.in)\nSRCS(use.cpp)\nEND()\n",
            "mod/template.cpp.in": '#include "dep.h"\n',
            "mod/dep.h": "",
            "mod/use.cpp": '#include "data.inc"\n',
        }
        lib.tool_program(files, "tools/archiver", "archiver")
        graph = lib.make(files, "mod", "--dist")
        producer = lib.node_by_output(graph, "$(B)/mod/data.inc")
        compiled = lib.node_by_output(graph, "$(B)/mod/use.cpp.o")
        self.assertEqual(set(compiled["inputs"]), {
            "$(S)/mod/use.cpp", "$(B)/mod/data.inc", "$(S)/mod/template.cpp.in", "$(S)/mod/dep.h",
        })
        self.assertIn(producer["uid"], compiled["deps"])

    def test_proto_compile_uses_generated_headers(self):
        files = proto_files()
        files.update({
            "p/ya.make": (
                "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\nSRCS(a.proto b.proto)\nEND()\n"
            ),
            "p/a.proto": 'import "p/b.proto";\n',
            "p/b.proto": 'import "google/protobuf/descriptor.proto";\n',
        })
        for args in ((), ("--dist",)):
            with self.subTest(args=args):
                graph = lib.make(files, "p", *args)
                producer = lib.node_by_output(graph, "$(B)/p/a.pb.h")
                compiled = lib.node_by_output(graph, "$(B)/p/a.pb.cc.o")
                self.assertTrue({
                    "$(S)/p/a.proto", "$(S)/p/b.proto", WRAPPER,
                    f"{PB_RUNTIME}/descriptor.proto",
                } <= set(producer["inputs"]))
                self.assertTrue({
                    "$(B)/p/a.pb.cc", "$(B)/p/a.pb.h", "$(B)/p/b.pb.h",
                    f"{PB_RUNTIME}/descriptor.pb.h",
                } <= set(compiled["inputs"]))
                self.assertTrue(set(compiled["inputs"]).isdisjoint({
                    "$(S)/p/a.proto", "$(S)/p/b.proto", WRAPPER,
                    f"{PB_RUNTIME}/descriptor.proto",
                }))
                self.assertIn(producer["uid"], compiled["deps"])

    def test_proto_does_not_consume_cpp_induced_dependencies(self):
        files = proto_files()
        files.update({
            "p/ya.make": "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\nRUN_PROGRAM(tool OUT generated.proto)\nSRCS(generated.proto)\nEND()\n",
            "app/ya.make": LIBRARY + "PEERDIR(p)\nSRCS(use.cpp)\nEND()\n",
            "app/use.cpp": '#include "p/generated.pb.h"\n#include "runtime/config.h"\n',
            "runtime/config.h": '#include "config"\n',
            "runtime/config": '#include "detail.h"\n',
            "runtime/detail.h": "",
        })
        lib.tool_program(files, "tool", "tool")
        files["tool/ya.make"] = files["tool/ya.make"].replace(
            "END()", "INDUCED_DEPS(h+cpp runtime/config.h)\nEND()"
        )
        graph = lib.make(files, "app", "--dist")
        proto = lib.node_by_output(graph, "$(B)/p/generated.pb.h")
        self.assertNotIn("$(S)/runtime/config.h", proto["inputs"])
        compiled = lib.node_by_output(graph, "$(B)/app/use.cpp.o")
        self.assertTrue({
            "$(S)/runtime/config.h", "$(S)/runtime/config", "$(S)/runtime/detail.h",
        } <= set(compiled["inputs"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
