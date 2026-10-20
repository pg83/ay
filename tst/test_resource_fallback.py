import unittest

import lib
from test_resource_raw import py3_library


class ResourceFallbackTests(unittest.TestCase):
    def test_large_yasm_batch_joins_chunks_in_reverse_order(self):
        entries = " ".join(f"- conftest.py/{i}=value{i}" for i in range(80))
        files = py3_library(f"RESOURCE({entries})\n")
        lib.tool_program(files, "tools/rorescompiler", "rorescompiler")
        lib.tool_program(files, "contrib/tools/yasm", "yasm")
        graph = lib.make(files, "lib", "--target-platform", "default-linux-x86_64")
        generated = [
            n["outputs"][0] for n in graph["graph"]
            if n["kv"]["p"] == "PR" and n["outputs"][0].endswith(".yasm")
        ]
        self.assertGreater(len(generated), 1)
        joined = next(
            n for n in graph["graph"] if n["kv"]["p"] == "JS"
            and n["outputs"][0].endswith(".yasm")
        )
        self.assertEqual(joined["cmds"][0]["cmd_args"][5:-1], list(reversed(generated)))
        obj = joined["outputs"][0][:-5] + ".o"
        self.assertIn(obj, lib.node_by_output(graph, "$(B)/lib/libpy3lib.global.a")["inputs"])

    def test_python_resources_disable_compression_on_x86(self):
        files = py3_library("PY_SRCS(conftest.py)\n")
        files["lib/conftest.py"] = ""
        graph = lib.make(files, "lib", "--target-platform", "default-linux-x86_64")
        raw = next(n for n in graph["graph"] if n["outputs"][0].endswith("_raw.auxcpp"))
        self.assertIn("$(S)/lib/conftest.py", raw["inputs"])
        self.assertFalse(any(n["outputs"][0].endswith(".yasm") for n in graph["graph"]))

    def test_resources_use_platform_resource_packer(self):
        for arch in ("aarch64", "x86_64"):
            with self.subTest(arch=arch):
                files = py3_library("RESOURCE(tests/conftest.py test)\n")
                files["lib/tests/conftest.py"] = "value = 1\n"
                lib.tool_program(files, "tools/rorescompiler", "rorescompiler")
                lib.tool_program(files, "contrib/tools/yasm", "yasm")
                graph = lib.make(files, "lib", "--target-platform", f"default-linux-{arch}")
                generated = [
                    n for n in graph["graph"] if n["kv"]["p"] == "PR"
                    and n["outputs"][0].startswith("$(B)/lib/")
                ]
                if arch == "aarch64":
                    raw = next(n for n in generated if n["outputs"][0].endswith("_raw.auxcpp"))
                    self.assertIn("$(S)/lib/tests/conftest.py", raw["inputs"])
                    lib.node_by_output(graph, raw["outputs"][0] + ".o")
                else:
                    yasm = next(n for n in generated if n["outputs"][0].endswith(".yasm"))
                    self.assertIn("$(S)/lib/tests/conftest.py", yasm["inputs"])
                    asm = lib.node_by_output(graph, yasm["outputs"][0][:-5] + ".o")
                    self.assertIn(yasm["uid"], asm["deps"])
                    self.assertTrue(any(o.endswith(".rodata") for o in asm["inputs"]))
                    cpp = next(n for n in generated if n["outputs"][0].endswith(".auxcpp"))
                    self.assertEqual(cpp["cmds"][0]["cmd_args"][0], "$(B)/tools/rorescompiler/rorescompiler")
                    lib.node_by_output(graph, cpp["outputs"][0] + ".o")


if __name__ == "__main__":
    unittest.main()
