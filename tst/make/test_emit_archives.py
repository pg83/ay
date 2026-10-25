import unittest

from tst import lib


class EmitArchivesTest(unittest.TestCase):
    def test_direct_build_sources_keep_order_before_automatic_outputs(self):
        for sources, shadow, expected in (
            ("${BINDIR}/generated.cpp plain.cpp", False, ["generated", "plain", "dummy"]),
            ("plain.cpp ${BINDIR}/generated.cpp", False, ["plain", "generated", "dummy"]),
            ("generated.cpp plain.cpp", False, ["generated", "plain", "dummy"]),
            ("generated.cpp plain.cpp", True, ["generated", "plain", "dummy"]),
        ):
            with self.subTest(sources=sources, shadow=shadow):
                files = {
                    "mod/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}"
                        "RUN_PYTHON3(gen.py OUT dummy.cpp OUT_NOAUTO generated.cpp)\n"
                        f"SRCS({sources})\nEND()\n",
                    "mod/plain.cpp": "int plain;\n",
                    "mod/gen.py": "",
                }
                if shadow:
                    files["mod/generated.cpp"] = "int source;\n"
                graph = lib.make(files, "mod")
                producer = lib.node_by_output(graph, "$(B)/mod/dummy.cpp")
                archive = lib.node_by_output(graph, "$(B)/mod/libmod.a")
                arguments = archive["cmds"][0]["cmd_args"]
                members = [arg for arg in arguments if arg.endswith(".cpp.o")]
                self.assertEqual(members, [f"$(B)/mod/{name}.cpp.o" for name in expected])
                for name in ("generated", "plain", "dummy"):
                    compiled = lib.node_by_output(graph, f"$(B)/mod/{name}.cpp.o")
                    self.assertIn(compiled["outputs"][0], archive["inputs"])
                    self.assertIn(compiled["uid"], archive["deps"])
                    if name == "dummy" or name == "generated" and not shadow:
                        self.assertIn(f"$(B)/mod/{name}.cpp", compiled["inputs"])
                        self.assertIn(producer["uid"], compiled["deps"])

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
