import unittest

import lib


class PackagingTests(unittest.TestCase):
    def test_archive_extraction_uses_generated_archive(self):
        graph = lib.make({
            "pkg/ya.make": """UNION()
FROM_ARCHIVE(bundle.tar PREFIX unpack RENAME original EXECUTABLE OUT bin/tool)
FROM_SANDBOX(FILE 123 OUT bundle.tar)
END()
""",
        }, "pkg")
        fetch = lib.only_node_by_kind(graph, "SB")
        extract = lib.only_node_by_kind(graph, "FA")
        package = lib.only_node_by_kind(graph, "UN")
        self.assertEqual(extract["cmds"][0]["cwd"], "$(B)/pkg")
        self.assertEqual(extract["cmds"][0]["cmd_args"][1:], [
            "$(S)/build/scripts/fetch_from_archive.py",
            "--archive", "$(B)/pkg/bundle.tar", "--file-name", "bundle.tar-",
            "--untar-to", "unpack", "--rename", "original", "--executable",
            "--", "bin/tool",
        ])
        self.assertIn("$(B)/pkg/bundle.tar", extract["inputs"])
        self.assertIn("$(S)/build/scripts/fetch_from.py", extract["inputs"])
        self.assertIn(fetch["uid"], extract["deps"])
        self.assertIn(extract["uid"], package["deps"])
        self.assertIn("$(B)/pkg/bin/tool", package["outputs"])

    def test_unused_archive_extraction_stays_lazy(self):
        graph = lib.make({
            "mod/ya.make": f"""LIBRARY()
{lib.BARE_MODULE}
FROM_ARCHIVE(data.tar OUT_NOAUTO unused.txt)
SRCS(main.cpp)
END()
""",
            "mod/data.tar": "archive",
            "mod/main.cpp": "int value;\n",
        }, "mod")
        self.assertFalse(any(n["kv"]["p"] == "FA" for n in graph["graph"]))

    def test_archive_output_includes_reach_compile(self):
        graph = lib.make({
            "mod/ya.make": f"""LIBRARY()
{lib.BARE_MODULE}
FROM_ARCHIVE(data.tar OUT generated.h OUTPUT_INCLUDES dependency.h)
SRCS(main.cpp)
END()
""",
            "mod/data.tar": "archive",
            "mod/dependency.h": "int value;\n",
            "mod/main.cpp": '#include "generated.h"\n',
        }, "mod")
        extract = lib.only_node_by_kind(graph, "FA")
        compile = lib.node_by_output(graph, "$(B)/mod/main.cpp.o")
        self.assertIn("$(S)/mod/data.tar", extract["inputs"])
        self.assertIn(extract["uid"], compile["deps"])
        self.assertIn("$(S)/mod/dependency.h", compile["inputs"])


if __name__ == "__main__":
    unittest.main()
