import unittest

import lib
from test_python_srcs import PY3CC, cmd, keys, kvs, objcopy_nodes, objcopy_with_kv, python_base


MODULE = "grove/runner"


def fixture(body="USE_PYTHON3()\nPY_SRCS(__main__.py)\n", macro="PROGRAM(runner)"):
    files = python_base()
    for peer, marker in (
        ("library/python/runtime_py3", "RUNTIME"),
        ("library/python/runtime_py3/main", "MAIN"),
        ("contrib/tools/python3/Modules/_sqlite", "SQLITE"),
        ("library/python/testing/import_test", "IMPORT"),
        ("library/python/runtime/main", "MAIN2"),
        ("contrib/tools/python/src/Modules/_sqlite", "SQLITE2"),
        ("contrib/tools/python3_prev/Modules/_sqlite", "PREVIOUS"),
        ("grove/alternate/Modules/_sqlite", "ALTERNATE"),
    ):
        files[peer + "/ya.make"] = (
            "LIBRARY()\n" + lib.BARE_MODULE +
            f"CFLAGS(GLOBAL -DRENAMED_{marker})\nSRCS(marker.cpp)\nEND()\n"
        )
        files[peer + "/marker.cpp"] = f"int marker_{marker};\n"
    lib.tool_program(files, "tools/py2cc", "py2cc")
    lib.tool_program(files, "tools/fix_elf", "fix_elf")
    files[MODULE + "/ya.make"] = (
        macro + "\n" + lib.BARE_MODULE + "NO_PYTHON_INCLUDES()\n"
        "SRCS(anchor.cpp)\n" + body + "END()\n"
    )
    files[MODULE + "/anchor.cpp"] = "int anchor;\n"
    files[MODULE + "/__main__.py"] = "value = 1\n"
    files[MODULE + "/worker.py"] = "worker = 1\n"
    return files


def markers(graph):
    return {
        arg.removeprefix("-DRENAMED_")
        for node in graph["graph"] if node["kv"].get("p") == "CC"
        and any(out.startswith(f"$(B)/{MODULE}/anchor.cpp") for out in node["outputs"])
        for arg in node["cmds"][0]["cmd_args"] if arg.startswith("-DRENAMED_")
    }


def imports(graph):
    return [node for node in graph["graph"]
            if f"$(B)/{MODULE}/test-results/import_test/meta.json" in node["outputs"]]


class PythonSourceEntryTest(unittest.TestCase):
    def assert_bytecode(self, graph, name="__main__.py"):
        compiled = lib.node_by_output(graph, f"$(B)/{MODULE}/{name}.yapyc3")
        self.assertEqual(cmd(compiled),
                         f"{PY3CC} {MODULE}/{name}- $(S)/{MODULE}/{name} $(B)/{MODULE}/{name}.yapyc3")
        tool = lib.node_by_output(graph, "$(B)/tools/py3cc/py3cc")
        self.assertIn(tool["uid"], compiled["deps"])
        packed = objcopy_with_kv(graph, MODULE,
                                f"resfs/src/resfs/file/py/{MODULE}/{name}={MODULE}/{name}")
        self.assertEqual(keys(packed), [f"resfs/file/py/{MODULE}/{name}",
                                       f"resfs/file/py/{MODULE}/{name}.yapyc3"])
        self.assertIn(compiled["outputs"][0], packed["inputs"])
        self.assertIn(compiled["uid"], packed["deps"])
        return packed

    def test_program_callback_keeps_main_resources_runtime_and_import_test(self):
        for body in ("USE_PYTHON3()\nPY_SRCS(__main__.py)\n",
                     "PY_SRCS(__main__.py)\nUSE_PYTHON3()\n"):
            with self.subTest(body=body):
                graph = lib.make(fixture(body), MODULE, "-t")
                packed = self.assert_bytecode(graph)
                main = objcopy_with_kv(graph, MODULE, "PY_MAIN=grove.runner.__main__")
                binary = lib.node_by_output(graph, f"$(B)/{MODULE}/runner")
                self.assertIn(packed["uid"], binary["deps"])
                self.assertIn(main["uid"], binary["deps"])
                self.assertEqual(markers(graph), {"RUNTIME", "MAIN", "SQLITE", "IMPORT"})
                run, = imports(graph)
                self.assertIn(binary["uid"], run["deps"])
                self.assertIn(run["uid"], graph["result"])
                self.assertFalse(any("import_tracing/constructor" in out
                                     for n in graph["graph"] for out in n["outputs"]))

    def test_empty_callback_has_runtime_effect_but_no_source_producers(self):
        for callback, expected in (("", {"RUNTIME"}),
                                   ("PY_SRCS()\n", {"RUNTIME", "MAIN", "SQLITE", "IMPORT"})):
            with self.subTest(callback=callback):
                graph = lib.make(fixture("USE_PYTHON3()\n" + callback), MODULE, "-t")
                self.assertEqual(markers(graph), expected)
                self.assertEqual(len(imports(graph)), int(bool(callback)))
                self.assertEqual(objcopy_nodes(graph, MODULE), [])

    def test_worker_does_not_invent_main_and_library_has_no_entry_callback(self):
        graph = lib.make(fixture("USE_PYTHON3()\nPY_SRCS(worker.py)\n"), MODULE, "-t")
        self.assert_bytecode(graph, "worker.py")
        self.assertFalse(any("PY_MAIN=" in value for n in objcopy_nodes(graph, MODULE) for value in kvs(n)))
        graph = lib.make(fixture(macro="LIBRARY(bundle)"), MODULE, "-t")
        self.assert_bytecode(graph)
        self.assertEqual(markers(graph), {"RUNTIME"})
        self.assertEqual(imports(graph), [])
        self.assertFalse(any("PY_MAIN=" in value for n in objcopy_nodes(graph, MODULE) for value in kvs(n)))

    def test_dll_uses_entry_resources_without_program_import_check(self):
        graph = lib.make(fixture(macro="DLL(bundle)"), MODULE, "-t")
        self.assert_bytecode(graph)
        objcopy_with_kv(graph, MODULE, "PY_MAIN=grove.runner.__main__")
        self.assertEqual(markers(graph), {"RUNTIME", "MAIN", "SQLITE"})
        self.assertEqual(imports(graph), [])

    def test_shared_object_program_overrides_dll_module_type(self):
        graph = lib.make(fixture(macro="SO_PROGRAM(bundle)"), MODULE, "-t")
        self.assert_bytecode(graph)
        objcopy_with_kv(graph, MODULE, "PY_MAIN=grove.runner.__main__")
        self.assertEqual(markers(graph), {"RUNTIME", "MAIN", "SQLITE", "IMPORT"})

    def test_callback_respects_sqlite_import_and_arcadia_controls(self):
        for setting, flags, expected, check in (
            ("DISABLE(PYTHON_SQLITE3)\nNO_CHECK_IMPORTS()\n", [], {"RUNTIME", "MAIN"}, False),
            ("", ["-DCPP_ANALYSIS_MODE=yes"], {"RUNTIME", "MAIN", "SQLITE"}, False),
            ("SET(MODULE_TYPE LIBRARY)\n", [], {"RUNTIME"}, False),
        ):
            with self.subTest(setting=setting, flags=flags):
                graph = lib.make(fixture("USE_PYTHON3()\n" + setting + "PY_SRCS()\n"), MODULE, "-t", *flags)
                self.assertEqual(markers(graph), expected)
                self.assertEqual(len(imports(graph)), int(check))

    def test_external_python_callback_skips_main_and_sqlite(self):
        graph = lib.make(fixture("USE_PYTHON3()\nSET(USE_ARCADIA_PYTHON no)\nPY_SRCS(worker.py)\n"), MODULE, "-t")
        self.assert_bytecode(graph, "worker.py")
        self.assertEqual(markers(graph) & {"MAIN", "SQLITE", "IMPORT"}, {"IMPORT"})
        self.assertEqual(len(imports(graph)), 1)
        self.assertFalse(any("PY_MAIN=" in value for n in objcopy_nodes(graph, MODULE) for value in kvs(n)))

    def test_program_descendant_uses_python_callback_without_allocator_proxy(self):
        files = fixture("ALLOCATOR(FAKE)\nUSE_PYTHON3()\nPY_SRCS(__main__.py)\n", macro="YT_UNITTEST(runner)")
        for peer in ("library/cpp/testing/unittest_main", "yt/cpp/mapreduce/tests/yt_initialize_hook"):
            files[peer + "/ya.make"] = "LIBRARY()\n" + lib.BARE_MODULE + "END()\n"
        graph = lib.make(files, MODULE, "-t")
        packed = self.assert_bytecode(graph)
        main = objcopy_with_kv(graph, MODULE, "PY_MAIN=grove.runner.__main__")
        self.assertEqual(markers(graph), {"RUNTIME", "MAIN", "SQLITE", "IMPORT"})
        self.assertEqual(len(imports(graph)), 1)
        binary = lib.node_by_output(graph, f"$(B)/{MODULE}/runner")
        self.assertIn(packed["uid"], binary["deps"])
        self.assertIn(main["uid"], binary["deps"])

    def test_explicit_library_callback_type_keeps_sources_without_main(self):
        graph = lib.make(fixture("USE_PYTHON3()\nSET(MODULE_TYPE LIBRARY)\nPY_SRCS(__main__.py)\n"), MODULE, "-t")
        self.assert_bytecode(graph)
        self.assertEqual(markers(graph), {"RUNTIME"})
        self.assertEqual(imports(graph), [])
        self.assertFalse(any("PY_MAIN=" in value for n in objcopy_nodes(graph, MODULE) for value in kvs(n)))

    def test_callback_uses_selected_python_runtime(self):
        for setting, expected in (
            ("USE_PYTHON2()\n", {"MAIN2", "SQLITE2"}),
            ("USE_PYTHON3()\nSET(USE_PYTHON3_PREV yes)\n", {"RUNTIME", "MAIN", "PREVIOUS"}),
            ("USE_PYTHON3()\nSET(PYTHON3_TOOL_PEERDIR grove/alternate)\n", {"RUNTIME", "MAIN", "ALTERNATE"}),
        ):
            with self.subTest(setting=setting):
                graph = lib.make(fixture(setting + "NO_CHECK_IMPORTS()\nPY_SRCS()\n"), MODULE)
                self.assertEqual(markers(graph), expected)

    def test_host_tool_keeps_python_producers_and_runtime_in_host_context(self):
        files = fixture()
        files["grove/client/ya.make"] = (
            "LIBRARY()\n" + lib.BARE_MODULE +
            "SRCS(client.cpp)\nRUN_PROGRAM(grove/runner OUT generated.cpp)\nEND()\n"
        )
        files["grove/client/client.cpp"] = "int client;\n"
        graph = lib.make(files, "grove/client")
        packed = self.assert_bytecode(graph)
        binary = lib.node_by_output(graph, f"$(B)/{MODULE}/runner")
        self.assertEqual(binary["platform"], "default-linux-x86_64")
        self.assertEqual(packed["platform"], binary["platform"])
        generated = lib.node_by_output(graph, "$(B)/grove/client/generated.cpp")
        self.assertIn(binary["uid"], generated["deps"])
        self.assertIn(binary["outputs"][0], generated["inputs"])
        self.assertEqual(imports(graph), [])


if __name__ == "__main__":
    unittest.main()
