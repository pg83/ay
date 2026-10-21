import unittest

import lib
from test_python2 import CONTRIB_LIBS_PYTHON, python2_base
from test_python_srcs import python_base


class EmitArchivesTest(unittest.TestCase):
    def test_declared_source_order_is_preserved_in_each_python_context(self):
        files = python2_base()
        files.update(python_base())
        files["contrib/libs/python/ya.make"] = CONTRIB_LIBS_PYTHON
        files["mod/ya.make"] = (
            "PY23_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
            "SRCS(${ARCADIA_BUILD_ROOT}/mod/generated.cpp plain.cpp)\n"
            "RUN_PYTHON3(gen.py OUT_NOAUTO generated.cpp)\nEND()\n"
        )
        files["mod/gen.py"] = ""
        files["mod/plain.cpp"] = ""
        for directory, module in (("two", "PY2_PROGRAM"), ("three", "PY3_PROGRAM")):
            files[directory + "/ya.make"] = (
                module + "()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "PY_SRCS(MAIN main.py)\nPEERDIR(mod)\nEND()\n"
            )
            files[directory + "/main.py"] = ""
        graph = lib.make(files, "two", "three", "--sandboxing")
        producer = lib.node_by_output(graph, "$(B)/mod/generated.cpp")
        for suffix in ("py2", "py3"):
            with self.subTest(context=suffix):
                generated_path = f"$(B)/mod/generated.cpp.{suffix}.o"
                plain_path = f"$(B)/mod/plain.cpp.{suffix}.o"
                compiled = lib.node_by_output(graph, generated_path)
                self.assertIn(producer["uid"], compiled["deps"])
                self.assertIn("$(B)/mod/generated.cpp", compiled["inputs"])
                archives = [node for node in graph["graph"]
                            if node["kv"]["p"] == "AR" and generated_path in node["inputs"]]
                self.assertEqual(len(archives), 1)
                archive = archives[0]
                self.assertIn(compiled["uid"], archive["deps"])
                args = archive["cmds"][0]["cmd_args"]
                self.assertLess(args.index(generated_path), args.index(plain_path))

    def test_explicit_build_sources_keep_declared_archive_order(self):
        for sources, output_kind, expected, source_dir in (
            ("${ARCADIA_BUILD_ROOT}/mod/generated.cpp plain.cpp", "OUT_NOAUTO", ["generated", "plain"], None),
            ("plain.cpp ${ARCADIA_BUILD_ROOT}/mod/generated.cpp", "OUT_NOAUTO", ["plain", "generated"], None),
            ("generated.cpp plain.cpp", "OUT_NOAUTO", ["plain", "generated"], None),
            ("other.cpp plain.cpp generated.cpp", "OUT_NOAUTO", ["other", "plain", "generated"], None),
            ("plain.cpp", "OUT", ["plain", "generated"], None),
            ("generated.cpp plain.cpp", "OUT_NOAUTO", ["generated", "plain"], "mod"),
            ("generated.cpp plain.cpp", "OUT_NOAUTO", ["generated", "plain"], "shared"),
        ):
            with self.subTest(sources=sources):
                files = {
                    "mod/ya.make": (
                        "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                        + ("SRCDIR(shared)\n" if source_dir == "shared" else "")
                        + f"SRCS({sources})\n"
                        f"RUN_PYTHON3(gen.py {output_kind} generated.cpp)\nEND()\n"
                    ),
                    "mod/plain.cpp": "",
                    "mod/other.cpp": "",
                    "mod/gen.py": "",
                }
                if source_dir:
                    files[source_dir + "/generated.cpp"] = ""
                graph = lib.make(files, "mod", "--sandboxing")
                producer = lib.node_by_output(graph, "$(B)/mod/generated.cpp")
                generated = lib.node_by_output(graph, "$(B)/mod/generated.cpp.o")
                plain = lib.node_by_output(graph, "$(B)/mod/plain.cpp.o")
                archive = lib.node_by_output(graph, "$(B)/mod/libmod.a")
                self.assertIn(producer["uid"], generated["deps"])
                self.assertIn("$(B)/mod/generated.cpp", generated["inputs"])
                for compiled in (generated, plain):
                    self.assertIn(compiled["uid"], archive["deps"])
                    self.assertIn(compiled["outputs"][0], archive["inputs"])
                args = archive["cmds"][0]["cmd_args"]
                members = [arg for arg in args if arg.startswith("$(B)/mod/") and arg.endswith(".cpp.o")]
                self.assertEqual(members, [f"$(B)/mod/{name}.cpp.o" for name in expected])

    def test_plain_archive_keeps_members_on_producer(self):
        files = {
            "mod/ya.make": (
                "LIBRARY()\n"
                "NO_LIBC()\n"
                "NO_RUNTIME()\n"
                "NO_UTIL()\n"
                "SRCS(use.cpp)\n"
                "ARCHIVE(NAME data.inc payload.lst)\n"
                "END()\n"
            ),
            "mod/payload.lst": "row\n",
            "mod/use.cpp": '#include "data.inc"\n',
        }
        lib.tool_program(files, "tools/archiver", "archiver")
        graph = lib.make(files, "mod")
        use = lib.node_by_output(graph, "$(B)/mod/use.cpp.o")
        self.assertIn("$(B)/mod/data.inc", use["inputs"])
        self.assertNotIn("$(S)/mod/payload.lst", use["inputs"])

    def test_archive_by_keys_top_level(self):
        files = {
            "mod/ya.make": (
                "LIBRARY()\n"
                "NO_LIBC()\n"
                "NO_RUNTIME()\n"
                "NO_UTIL()\n"
                "SRCS(use.cpp)\n"
                "ARCHIVE_BY_KEYS(\n"
                "  NAME data.inc\n"
                "  KEYS /k1:/k2\n"
                "  a.txt\n"
                "  sub/b.txt\n"
                ")\n"
                "END()\n"
            ),
            "mod/a.txt": "alpha\n",
            "mod/sub/b.txt": "beta\n",
            "mod/use.cpp": '#include "data.inc"\n',
        }
        lib.tool_program(files, "tools/archiver", "archiver")
        graph = lib.make(files, "mod")
        archive = lib.node_by_output(graph, "$(B)/mod/data.inc")
        self.assertEqual(archive["kv"]["p"], "AR")
        self.assertEqual(archive["kv"]["pc"], "light-red")
        command = " ".join(archive["cmds"][0]["cmd_args"])
        self.assertIn("$(S)/mod/a.txt $(S)/mod/sub/b.txt", command)
        self.assertIn("-k /k1:/k2", command)
        self.assertIn("-o $(B)/mod/data.inc", command)
        self.assertNotIn("$(S)/mod/a.txt:", command)
        self.assertIn("$(S)/mod/a.txt", archive["inputs"])
        self.assertIn("$(S)/mod/sub/b.txt", archive["inputs"])

        use = lib.node_by_output(graph, "$(B)/mod/use.cpp.o")
        self.assertIn("-I$(B)/mod", use["cmds"][0]["cmd_args"])
        self.assertNotIn("$(S)/mod/a.txt", use["inputs"])
        self.assertNotIn("$(S)/mod/sub/b.txt", use["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
