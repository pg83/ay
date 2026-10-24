import unittest

import lib
from test_link_ld_flags import command_with
from test_python2 import python2_base
from test_python_srcs import python_base


BASE = lib.BARE_MODULE + "NO_PYTHON_INCLUDES()\nNO_IMPORT_TRACING()\nDISABLE(PYTHON_SQLITE3)\nNO_CHECK_IMPORTS()\nALLOCATOR(FAKE)\n"
CASES = {
    "default": ("", "", True),
    "disabled": ("", "DISABLE(STRIP)\n", False),
    "set_no": ("", "SET(STRIP no)\n", False),
    "empty": ("", "SET(STRIP)\n", False),
    "truthy": ("", "SET(STRIP true)\n", False),
    "enabled": ("", "ENABLE(STRIP)\n", True),
    "strip_disabled": ("", "STRIP()\nDISABLE(STRIP)\n", False),
    "disabled_strip": ("", "DISABLE(STRIP)\nSTRIP()\n", True),
    "no_strip": ("", "ENABLE(NO_STRIP)\n", False),
    "truthy_no_strip": ("", "SET(NO_STRIP true)\n", True),
    "clear_no_strip": ("", "ENABLE(NO_STRIP)\nDISABLE(NO_STRIP)\n", True),
    "header_overrides": ("DISABLE(STRIP)\n", "", True),
    "source_clears_debug": ("", "SET(NO_STRIP no)\n", True),
    "sanitizer": ("SET(SANITIZER_TYPE address)\n", "", False),
    "source_clears_sanitizer": ("SET(SANITIZER_TYPE address)\n", "SET(NO_STRIP no)\n", True),
}


def fixture(kind="PY3_PROGRAM", cases=CASES):
    files = python2_base() if kind == "PY2_PROGRAM" else python_base()
    roots = []
    for name, (before, body, _) in cases.items():
        module = "grove/" + name
        roots.append(module)
        source = "SRCS(main.cpp)\n" if kind in ("PROGRAM", "DLL", "LIBRARY") else "PY_SRCS(__main__.py)\n"
        files[module + "/ya.make"] = before + kind + "(seed)\n" + BASE + body + source + "END()\n"
        files[module + "/__main__.py"] = "print('seed')\n"
        files[module + "/main.cpp"] = "int main(){return 0;}\n"
    lib.tool_program(files, "tools/fix_elf", "fix_elf")
    return files, roots


def output(kind, module, platform="linux"):
    if kind == "DLL":
        return "$(B)/" + module + "/libseed." + ("dylib" if platform == "darwin" else "so")
    if kind == "LIBRARY":
        return "$(B)/" + module + "/libseed.a"
    return "$(B)/" + module + "/seed"


def stripped(node):
    return "-Wl,--strip-all" in command_with(node, "link_dyn_lib.py" if any(o.endswith((".so", ".dylib")) for o in node["outputs"]) else "link_exe.py")


class EffectiveStripTest(unittest.TestCase):
    def graph(self, kind, cases, flags=("-r",), platform="linux"):
        files, roots = fixture(kind, cases)
        graph = lib.make(files, roots[-1], *roots[:-1], *flags, "--target-platform", "default-" + platform + "-x86_64")
        ids = {n["uid"] for n in graph["graph"]}
        self.assertTrue(graph["result"])
        self.assertLessEqual(set(graph["result"]), ids)
        self.assertTrue(all(set(n["deps"]) <= ids for n in graph["graph"]))
        return graph

    def test_python_header_then_source_setters(self):
        for kind in ("PY3_PROGRAM", "PY2_PROGRAM"):
            with self.subTest(kind=kind):
                graph = self.graph(kind, CASES)
                for name, (_, _, expected) in CASES.items():
                    with self.subTest(case=name):
                        node = lib.node_by_output(graph, output(kind, "grove/" + name))
                        self.assertEqual(stripped(node), expected)
                        self.assertTrue(node["deps"])

    def test_cpp_and_shared_libraries_use_the_same_effective_scalar(self):
        cases = {name: CASES[name] for name in ("default", "disabled", "enabled", "strip_disabled", "disabled_strip", "truthy", "truthy_no_strip", "clear_no_strip")}
        for kind in ("PROGRAM", "DLL"):
            with self.subTest(kind=kind):
                graph = self.graph(kind, cases)
                for name, (_, _, expected) in cases.items():
                    expected = expected and name not in ("default", "truthy_no_strip", "clear_no_strip")
                    self.assertEqual(stripped(lib.node_by_output(graph, output(kind, "grove/" + name))), expected, (kind, name))

    def test_debug_defaults_can_be_overridden_in_source(self):
        cases = {name: CASES[name] for name in ("default", "source_clears_debug", "disabled_strip")}
        for flags in (("-d",), ("--build", "fastdebug")):
            graph = self.graph("PY3_PROGRAM", cases, flags)
            for name in cases:
                self.assertEqual(stripped(lib.node_by_output(graph, output("PY3_PROGRAM", "grove/" + name))), name == "source_clears_debug")

    def test_header_and_body_override_cli_in_order(self):
        cases = {name: CASES[name] for name in ("default", "disabled", "source_clears_debug")}
        for flags, enabled in ((("-r", "-DSTRIP=no"), {"default", "source_clears_debug"}), (("-r", "-DNO_STRIP=yes"), {"source_clears_debug"}), (("-d", "-DNO_STRIP=no"), {"source_clears_debug"})):
            graph = self.graph("PY3_PROGRAM", cases, flags)
            for name in cases:
                self.assertEqual(stripped(lib.node_by_output(graph, output("PY3_PROGRAM", "grove/" + name))), name in enabled)

    def test_archive_and_python_self_library_do_not_get_link_flags(self):
        cases = {"enabled": CASES["enabled"]}
        for kind in ("LIBRARY", "PY3_PROGRAM"):
            graph = self.graph(kind, cases)
            archives = [n for n in graph["graph"] if any(o.startswith("$(B)/grove/") and o.endswith(".a") for o in n["outputs"])]
            self.assertTrue(archives)
            self.assertTrue(all("-Wl,--strip-all" not in c["cmd_args"] for n in archives for c in n["cmds"]))

    def test_non_linux_linkers_keep_their_existing_strip_mode(self):
        cases = {"enabled": CASES["enabled"], "disabled": CASES["disabled"]}
        for platform in ("darwin", "windows"):
            graph = self.graph("PROGRAM", cases, platform=platform)
            for name in cases:
                node = lib.node_by_output(graph, output("PROGRAM", "grove/" + name, platform))
                self.assertTrue(all("-Wl,--strip-all" not in cmd["cmd_args"] for cmd in node["cmds"]))


if __name__ == "__main__":
    unittest.main()
