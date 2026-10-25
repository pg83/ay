import unittest

from tst import lib


BASE = lib.BARE_MODULE + "DISABLE(NEED_PLATFORM_PEERDIRS)\n"
CASES = {
    "plain": ("PY_ANY_MODULE", "seed", "", "", False),
    "py2": ("PY_ANY_MODULE", "seed", "", "PYTHON2_MODULE()\n", True),
    "py3": ("PY_ANY_MODULE", "seed", "", "PYTHON3_MODULE()\n", True),
    "two_then_three": ("PY_ANY_MODULE", "seed", "", "PYTHON2_MODULE()\nPYTHON3_MODULE()\n", True),
    "three_then_two": ("PY_ANY_MODULE", "seed", "", "PYTHON3_MODULE()\nPYTHON2_MODULE()\n", True),
    "versioned": ("PY_ANY_MODULE", "seed 7 2 PREFIX plug_", "", "PYTHON3_MODULE()\nEXPORTS_SCRIPT(api.map)\n", True),
    "fallback": ("PY_ANY_MODULE", "", "", "", False),
    "peer_override": ("PY_ANY_MODULE", "seed", "", "PYTHON3_MODULE()\nSET(PEERDIR_TAGS PY2_NATIVE __EMPTY__)\n", True),
    "runtime_before": ("PY_ANY_MODULE", "seed", "SET(IS_CROSS_TOOLS yes)\n", "PYTHON3_MODULE()\n", False),
    "runtime_after": ("PY_ANY_MODULE", "seed", "", "PYTHON3_MODULE()\nSET(IS_CROSS_TOOLS yes)\n", False),
    "runtime_py2": ("PY_ANY_MODULE", "seed", "SET(IS_CROSS_TOOLS yes)\n", "PYTHON2_MODULE()\n", False),
    "runtime_disabled": ("PY_ANY_MODULE", "seed", "SET(IS_CROSS_TOOLS yes)\n", "PYTHON3_MODULE()\nSET(IS_CROSS_TOOLS no)\n", False),
    "runtime_both": ("PY_ANY_MODULE", "seed", "SET(IS_CROSS_TOOLS yes)\n", "PYTHON2_MODULE()\nPYTHON3_MODULE()\n", False),
    "runtime_reverse": ("PY_ANY_MODULE", "seed", "SET(IS_CROSS_TOOLS yes)\n", "PYTHON3_MODULE()\nPYTHON2_MODULE()\n", False),
    "peer_both": ("PY_ANY_MODULE", "seed", "", "PYTHON3_MODULE()\nSET(PEERDIR_TAGS PY3_NATIVE PY2_NATIVE __EMPTY__)\n", True),
    "runtime_previous": ("PY_ANY_MODULE", "seed", "SET(IS_CROSS_TOOLS yes)\nSET(USE_PYTHON3_PREV yes)\n", "PYTHON3_MODULE()\n", False),
    "generated": ("PY_ANY_MODULE", "seed", "", "PYTHON3_MODULE()\nCOPY_FILE(main.cpp staged.cpp)\nSRCS(staged.cpp GLOBAL anchor.cpp)\nRESOURCE(payload.txt orchard_payload)\n", True),
    "runtime_custom": ("PY_ANY_MODULE", "seed", "SET(IS_CROSS_TOOLS yes)\n", "PYTHON3_MODULE()\nSET(PYTHON3_TOOL_PEERDIR grove/python3custom)\n", False),
    "system": ("PY_ANY_MODULE", "seed", "SET(USE_ARCADIA_PYTHON no)\nSET(USE_SYSTEM_PYTHON 3.11)\n", "PYTHON3_MODULE()\n", False),
    "external": ("PY_ANY_MODULE", "seed", "SET(USE_ARCADIA_PYTHON no)\nSET(PYTHON_INCLUDE -DEXTERNAL_PYTHON)\n", "PYTHON3_MODULE()\n", False),
    "legacy": ("PY2MODULE", "seed", "", "", True),
}


def fixture(cases=CASES):
    files = {
        "grove/plain/ya.make": "LIBRARY()\n" + BASE + "SRCS(plain.cpp)\nEND()\n",
        "grove/plain/plain.cpp": "int plain;\n",
        "grove/native/ya.make": "PY23_NATIVE_LIBRARY()\n" + BASE + "SRCS(native.cpp)\nIF(PYTHON3)\nCFLAGS(-DNATIVE_THREE)\nELSE()\nCFLAGS(-DNATIVE_TWO)\nENDIF()\nEND()\n",
        "grove/native/native.cpp": "int native;\n",
        "contrib/libs/python/Include/Python.h": "",
        "contrib/libs/python/Include_prev/Python.h": "",
    }
    for path, marker in (("contrib/tools/python/lib", "TWO"), ("contrib/tools/python3", "THREE"), ("contrib/tools/python3/Lib", "THREE_LIB"), ("build/platform/python", "SYSTEM"), ("contrib/tools/python3_prev", "PREVIOUS"), ("contrib/tools/python3_prev/Lib", "PREVIOUS_LIB"), ("grove/python3custom", "CUSTOM"), ("grove/python3custom/Lib", "CUSTOM_LIB")):
        files[path + "/ya.make"] = "LIBRARY()\n" + BASE + "SRCS(runtime.cpp)\nCFLAGS(-DRUNTIME_" + marker + ")\nEND()\n"
        files[path + "/runtime.cpp"] = "int runtime;\n"
    files["build/platform/python/ya.make"] = "RESOURCES_LIBRARY()\nEND()\n"
    roots = []
    for name, (kind, args, before, body, native) in cases.items():
        root = "orchard/" + name
        roots.append(root)
        files[root + "/ya.make"] = (before + kind + "(" + args + ")\n" + BASE + body
            + "CFLAGS(-DSELECTED_TAG=$MODULE_TAG -DSELECTED_PY=$PYTHON_TYPE_FOR_CYTHON -DSELECTED_LANG=$MODULE_LANG -DSELECTED_ANTLR=$ANTLR_PYTHON)\n"
            + "SRCS(main.cpp)\nPEERDIR(grove/plain" + (" grove/native" if native else "") + ")\nEND()\n")
        files[root + "/main.cpp"] = "int entry;\n"
        files[root + "/anchor.cpp"] = "int anchor;\n"
        files[root + "/payload.txt"] = "payload\n"
        files[root + "/api.map"] = "{ global: entry; local: *; };\n"
    files["library/cpp/resource/ya.make"] = "LIBRARY()\n" + BASE + "END()\n"
    lib.tool_program(files, "tools/rescompressor", "rescompressor")
    lib.tool_program(files, "tools/rescompiler", "rescompiler")
    lib.tool_program(files, "tools/fix_elf", "fix_elf")
    return files, roots


class PythonAnyModuleTest(unittest.TestCase):
    def graph(self, cases, reverse=False):
        files, roots = fixture({name: CASES[name] for name in cases})
        graph = lib.make(files, *(list(reversed(roots)) if reverse else roots),
                         "--sandboxing", "--target-platform", "default-linux-x86_64")
        ids = {node["uid"] for node in graph["graph"]}
        self.assertTrue(graph["result"])
        self.assertLessEqual(set(graph["result"]), ids)
        for node in graph["graph"]:
            self.assertLessEqual(set(node["deps"]), ids)
        return graph

    def linked(self, graph, name, output="seed.so"):
        node = lib.node_by_output(graph, "$(B)/orchard/" + name + "/" + output)
        self.assertIn(node["uid"], graph["result"])
        compiled = lib.node_by_output(graph, "$(B)/orchard/" + name + "/main.cpp.pic.o")
        self.assertIn(compiled["uid"], node["deps"])
        self.assertIn(compiled["outputs"][0], node["inputs"])
        self.assertEqual(compiled["inputs"], ["$(S)/orchard/" + name + "/main.cpp"])
        argv = [arg for command in compiled["cmds"] for arg in command["cmd_args"]]
        link = next(command["cmd_args"] for command in node["cmds"]
                    if any(arg.endswith("/link_dyn_lib.py") for arg in command["cmd_args"]))
        self.assertIn("-Wl,-Bsymbolic", link)
        return node, argv, link

    def test_selectors_preserve_cpp_language_and_last_peer_tag(self):
        cases = ("plain", "py2", "py3", "two_then_three", "three_then_two", "peer_override", "peer_both", "legacy")
        for reverse in (False, True):
            graph = self.graph(cases, reverse)
            for name in cases:
                with self.subTest(name=name, reverse=reverse):
                    node, argv, link = self.linked(graph, name)
                    two = name in ("py2", "three_then_two", "legacy")
                    tag = "DLL" if name == "plain" else ("PY2_NATIVE" if two else "PY3_NATIVE")
                    self.assertIn("-DSELECTED_TAG=" + tag, argv)
                    self.assertIn("-DSELECTED_LANG=" + ("PY2" if name == "legacy" else "CPP"), argv)
                    self.assertIn("-DSELECTED_PY=" + ("PY2" if two else "PY3"), argv)
                    self.assertIn("-DSELECTED_ANTLR=" + ("Python2" if two or name == "plain" else "Python3"), argv)
                    self.assertEqual("-DUSE_PYTHON3" in argv, name in ("py3", "two_then_three", "three_then_two", "peer_override", "peer_both"))
                    self.assertEqual(any(arg.startswith("-DARCADIA_PYTHON_UNICODE_SIZE=") for arg in argv),
                                     name in ("py2", "two_then_three", "three_then_two", "legacy"))
                    self.assertIn("grove/plain/libgrove-plain.a", link)
                    if name != "plain":
                        archive = "libgrove-native.a" if two or name in ("peer_override", "peer_both") else "libpy3cgrove-native.a"
                        peer = lib.node_by_output(graph, "$(B)/grove/native/" + archive)
                        self.assertIn(peer["uid"], node["deps"])
                        self.assertIn(peer["outputs"][0].removeprefix("$(B)/"), link)

    def test_runtime_guards_use_final_environment_and_keep_both_invocations(self):
        cases = ("runtime_before", "runtime_after", "runtime_disabled", "runtime_py2", "runtime_both", "runtime_reverse", "runtime_custom", "runtime_previous")
        for reverse in (False, True):
            graph = self.graph(cases, reverse)
            for name in cases:
                with self.subTest(name=name, reverse=reverse):
                    node, _, link = self.linked(graph, name)
                    expected = []
                    if name in ("runtime_py2", "runtime_both", "runtime_reverse"):
                        expected.append("$(B)/contrib/tools/python/lib/libtools-python-lib.a")
                    if name in ("runtime_before", "runtime_after", "runtime_both", "runtime_reverse"):
                        expected += ["$(B)/contrib/tools/python3/libcontrib-tools-python3.a", "$(B)/contrib/tools/python3/Lib/libtools-python3-Lib.a"]
                    if name == "runtime_previous":
                        expected += ["$(B)/contrib/tools/python3_prev/libcontrib-tools-python3_prev.a", "$(B)/contrib/tools/python3_prev/Lib/libtools-python3_prev-Lib.a"]
                    if name == "runtime_custom":
                        expected += ["$(B)/grove/python3custom/libgrove-python3custom.a", "$(B)/grove/python3custom/Lib/libgrove-python3custom-Lib.a"]
                    actual = ["$(B)/" + arg for arg in link if arg.endswith(".a") and (arg.startswith("contrib/tools/python") or arg.startswith("grove/python3custom/"))]
                    self.assertEqual(actual, expected)
                    for output in expected:
                        self.assertIn(lib.node_by_output(graph, output)["uid"], node["deps"])

    def test_dll_name_version_exports_and_external_headers(self):
        graph = self.graph(("fallback", "versioned", "system", "external"))
        self.linked(graph, "fallback", "orchard-fallback.so")
        node, _, link = self.linked(graph, "versioned", "plug_seed.so")
        self.assertEqual(node["outputs"], ["$(B)/orchard/versioned/plug_seed.so", "$(B)/orchard/versioned/plug_seed.so.7"])
        self.assertIn("--soname", link)
        self.assertIn("$(B)/orchard/versioned/plug_seed.so.7", link)
        self.assertIn("$(S)/orchard/versioned/api.map", node["inputs"])
        self.assertIn("-Wl,--version-script=$(S)/orchard/versioned/api.map", link)
        _, argv, _ = self.linked(graph, "external")
        self.assertIn("-DEXTERNAL_PYTHON", argv)
        self.assertNotIn("-DUSE_PYTHON3", argv)
        _, argv, _ = self.linked(graph, "system")
        self.assertNotIn("-DUSE_PYTHON3", argv)


    def test_header_exports_and_incompatible_native_peer_are_rejected(self):
        from tst.make.test_link_dynamic_library import make_raw
        for body, declaration, message in (
            ("", "PY_ANY_MODULE(seed EXPORTS api.map)", "EXPORTS keyword"),
            ("PYTHON3_MODULE()\nSET(PEERDIR_TAGS CPP_PROTO __EMPTY__)\n", "PY_ANY_MODULE(seed)", "no compatible Python header peer"),
        ):
            with self.subTest(declaration=declaration, body=body):
                files, _ = fixture({"py3": CASES["py3"]})
                files["orchard/py3/ya.make"] = declaration + "\n" + BASE + body + "SRCS(main.cpp)\nPEERDIR(grove/native)\nEND()\n"
                result = make_raw(files, "orchard/py3")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)


    def test_generated_sources_and_resources_keep_genuine_producer_links(self):
        graph = self.graph(("generated",))
        linked, _, _ = self.linked(graph, "generated")
        copied = lib.node_by_output(graph, "$(B)/orchard/generated/staged.cpp")
        compiled = lib.node_by_output(graph, "$(B)/orchard/generated/staged.cpp.pic.o")
        self.assertIn("$(S)/orchard/generated/main.cpp", copied["inputs"])
        self.assertIn(copied["uid"], compiled["deps"])
        self.assertIn(compiled["uid"], linked["deps"])
        packed = [node for node in graph["graph"] if any(output.startswith("$(B)/orchard/generated/objcopy_") for output in node["outputs"])]
        self.assertEqual(len(packed), 1)
        self.assertIn("$(S)/orchard/generated/payload.txt", packed[0]["inputs"])
        self.assertIn(packed[0]["uid"], linked["deps"])
        self.assertIn(packed[0]["outputs"][0], linked["inputs"])
        global_object = lib.node_by_output(graph, "$(B)/orchard/generated/anchor.cpp.pic.o")
        self.assertIn(global_object["uid"], linked["deps"])

    def test_native_restricted_body_macros_are_rejected(self):
        from tst.make.test_link_dynamic_library import make_raw
        for kind, macros in (("PY_ANY_MODULE", ("USE_PYTHON2()", "USE_PYTHON3()", "PY_SRCS(test.py)", "PY_MAIN(test:main)")),
                             ("PY2MODULE", ("PYTHON3_MODULE()", "PYTHON3_ADDINCL()"))):
            for macro in macros:
                with self.subTest(kind=kind, macro=macro):
                    files, _ = fixture({"plain": CASES["plain"]})
                    files["orchard/plain/ya.make"] = kind + "(seed)\n" + BASE + macro + "\nEND()\n"
                    result = make_raw(files, "orchard/plain")
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn("is restricted", result.stderr)


if __name__ == "__main__":
    unittest.main()
