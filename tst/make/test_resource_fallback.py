import unittest

from tst import lib
from tst.make.test_resource_raw import py3_library


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

if __name__ == "__main__":
    unittest.main()
