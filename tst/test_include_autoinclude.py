import unittest

import lib


def library(sources):
    return (
        "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
        f"SRCS({sources})\nEND()\n"
    )


class AutoincludeTest(unittest.TestCase):
    FILES = {
        "build/conf/autoincludes.json": '["a", "a/b", "deep/er/root"]',
        "build/internal/conf/autoincludes.json": '["x/y"]',
        "a/linters.make.inc": "CFLAGS(-DLINT_A)\n",
        "a/b/linters.make.inc": "CFLAGS(-DLINT_AB)\n",
        "x/y/linters.make.inc": "CFLAGS(-DLINT_XY)\n",
        "deep/er/root/linters.make.inc": "CFLAGS(-DLINT_DEEP)\n",
    }

    def lint_flags(self, module_dir):
        files = dict(self.FILES)
        files[f"{module_dir}/ya.make"] = library("s.cpp")
        files[f"{module_dir}/s.cpp"] = "int s(){return 0;}\n"
        graph = lib.make(files, module_dir)
        args = lib.node_by_output(graph, f"$(B)/{module_dir}/s.cpp.o")["cmds"][0]["cmd_args"]
        return [arg for arg in args if arg.startswith("-DLINT_")]

    def test_longest_root_prefix_selects_linters_include(self):
        cases = {
            "a/b/c": ["-DLINT_AB"],
            "a/b": ["-DLINT_AB"],
            "a/bc": ["-DLINT_A"],
            "a": ["-DLINT_A"],
            "x/y/z": ["-DLINT_XY"],
            "x": [],
            "deep/er": [],
            "deep/er/root/leaf": ["-DLINT_DEEP"],
            "other": [],
        }
        for module_dir, expected in cases.items():
            with self.subTest(module=module_dir):
                self.assertEqual(expected, self.lint_flags(module_dir))

    def test_missing_linters_include_is_skipped(self):
        files = {
            "build/conf/autoincludes.json": '["n"]',
            "n/m/ya.make": library("s.cpp"),
            "n/m/s.cpp": "int s(){return 0;}\n",
        }
        graph = lib.make(files, "n/m")
        args = lib.node_by_output(graph, "$(B)/n/m/s.cpp.o")["cmds"][0]["cmd_args"]
        self.assertEqual([], [arg for arg in args if arg.startswith("-DLINT_")])

    def test_malformed_index_is_fatal(self):
        files = {
            "build/conf/autoincludes.json": '{"not": "a list"}',
            "m/ya.make": library("s.cpp"),
            "m/s.cpp": "int s(){return 0;}\n",
        }
        with self.assertRaisesRegex(AssertionError, "autoinclude: parse build/conf/autoincludes.json"):
            lib.make(files, "m")


if __name__ == "__main__":
    unittest.main(verbosity=2)
