import unittest

import lib


class NonCanonicalModulePathTest(unittest.TestCase):
    def test_srcs_include_and_copy_file_spellings(self):
        files = {
            "m/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "INCLUDE(../common/flags.inc)\n"
                "INCLUDE(${ARCADIA_ROOT}/common/./more.inc)\n"
                "INCLUDE(sub/local.inc)\n"
                "INCLUDE(sub/slash.inc/)\n"
                "INCLUDE(${ARCADIA_ROOT}/root.inc)\n"
                "SRCS(sub/../a.cpp sub//b.cpp sub/./c.cpp ./e.cpp)\n"
                "COPY_FILE(src.h out/../dst.h)\n"
                "END()\n"
            ),
            "common/flags.inc": "CFLAGS(-DFROM_INCLUDE)\n",
            "common/more.inc": "CFLAGS(-DFROM_MORE)\n",
            "m/sub/local.inc": "CFLAGS(-DFROM_LOCAL)\n",
            "m/sub/slash.inc": "CFLAGS(-DFROM_SLASH)\n",
            # An INCLUDE inside a root-level file resolves against the root.
            "root.inc": "INCLUDE(nested.inc)\n",
            "nested.inc": "CFLAGS(-DFROM_NESTED)\n",
            "m/a.cpp": '#include "dst.h"\n',
            "m/sub/b.cpp": "",
            "m/sub/c.cpp": "",
            "m/e.cpp": "",
            "m/src.h": "",
            "build/scripts/fs_tools.py": "",
        }
        graph = lib.make(files, "m")
        objects = {
            "$(B)/m/a.cpp.o": "$(S)/m/a.cpp",
            "$(B)/m/_/sub/b.cpp.o": "$(S)/m/sub/b.cpp",
            "$(B)/m/_/sub/c.cpp.o": "$(S)/m/sub/c.cpp",
            "$(B)/m/e.cpp.o": "$(S)/m/e.cpp",
        }
        for output, source in objects.items():
            with self.subTest(output=output):
                node = lib.node_by_output(graph, output)
                self.assertEqual(source, node["inputs"][0])
                defines = [a for a in node["cmds"][0]["cmd_args"] if a.startswith("-DFROM_")]
                self.assertEqual(
                    [
                        "-DFROM_INCLUDE", "-DFROM_MORE", "-DFROM_LOCAL",
                        "-DFROM_SLASH", "-DFROM_NESTED",
                    ],
                    defines,
                )
        self.assertIn(
            "$(B)/m/dst.h", lib.node_by_output(graph, "$(B)/m/a.cpp.o")["inputs"]
        )
        self.assertEqual(
            ["$(S)/build/scripts/fs_tools.py", "$(S)/m/src.h"],
            lib.node_by_output(graph, "$(B)/m/dst.h")["inputs"],
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
