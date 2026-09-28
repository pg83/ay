import base64
import unittest

import lib


STUB_LIBRARY = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n"

PEER_STUBS = (
    "contrib/libs/python",
    "contrib/tools/python3/Modules/_sqlite",
    "library/cpp/malloc/jemalloc",
    "library/cpp/resource",
    "library/python/import_tracing/constructor",
    "library/python/runtime_py3/main",
    "library/python/testing/import_test",
)

TOOLS = (
    "tools/archiver",
    "tools/py3cc",
    "tools/py3cc/slow",
    "tools/rescompiler",
    "tools/rescompressor",
)

PY3CC = "$(B)/tools/py3cc/py3cc --slow-py3cc $(B)/tools/py3cc/slow/slow"


def python_base():
    files = {f"{path}/ya.make": STUB_LIBRARY for path in PEER_STUBS}
    for path in TOOLS:
        lib.tool_program(files, path, path.rsplit("/", 1)[-1])
    return files


def cmd(node):
    return " ".join(node["cmds"][0]["cmd_args"])


def kvs(node):
    args = node["cmds"][0]["cmd_args"]
    start = args.index("--kvs") + 1
    return args[start:]


def keys(node):
    args = node["cmds"][0]["cmd_args"]
    start = args.index("--keys") + 1
    out = []
    for arg in args[start:]:
        if arg.startswith("--"):
            break
        out.append(base64.b64decode(arg).decode())
    return out


def objcopy_nodes(graph, module):
    prefix = f"$(B)/{module}/objcopy_"
    return [
        node for node in graph["graph"]
        if any(out.startswith(prefix) for out in node["outputs"])
    ]


def objcopy_with_kv(graph, module, kv):
    nodes = [node for node in objcopy_nodes(graph, module) if kv in kvs(node)]
    if len(nodes) != 1:
        raise AssertionError(f"expected one objcopy with {kv!r}, got {len(nodes)}")
    return nodes[0]


class PySrcsTest(unittest.TestCase):
    def test_library_groups_key_resources_by_namespace(self):
        files = python_base()
        files.update({
            "lib/ya.make": (
                "PY3_LIBRARY()\n"
                "PY_SRCS(TOP_LEVEL top.py)\n"
                "PY_SRCS(NAMESPACE foo.bar ns.py pkg/mod.py stub.pyi)\n"
                "PY_SRCS(plain.py)\n"
                "END()\n"
            ),
            "lib/top.py": "",
            "lib/ns.py": "",
            "lib/pkg/mod.py": "",
            "lib/stub.pyi": "",
            "lib/plain.py": "",
        })
        graph = lib.make(files, "lib")

        top = lib.node_by_output(graph, "$(B)/lib/top.py.yapyc3")
        self.assertEqual(top["kv"]["p"], "PY")
        self.assertEqual(
            cmd(top),
            f"{PY3CC} lib/top.py- $(S)/lib/top.py $(B)/lib/top.py.yapyc3",
        )
        self.assertEqual(
            top["inputs"],
            ["$(B)/tools/py3cc/py3cc", "$(B)/tools/py3cc/slow/slow", "$(S)/lib/top.py"],
        )
        nested = lib.node_by_output(graph, "$(B)/lib/pkg/mod.py.zsw2.yapyc3")
        self.assertEqual(
            cmd(nested),
            f"{PY3CC} lib/pkg/mod.py- $(S)/lib/pkg/mod.py $(B)/lib/pkg/mod.py.zsw2.yapyc3",
        )

        top_res = objcopy_with_kv(graph, "lib", "resfs/src/resfs/file/py/top.py=lib/top.py")
        self.assertEqual(keys(top_res), ["resfs/file/py/top.py", "resfs/file/py/top.py.yapyc3"])
        self.assertIn("$(B)/lib/top.py.yapyc3", top_res["inputs"])

        ns_res = objcopy_with_kv(graph, "lib", "resfs/src/resfs/file/py/foo/bar/ns.py=lib/ns.py")
        self.assertEqual(kvs(ns_res), [
            "resfs/src/resfs/file/py/foo/bar/ns.py=lib/ns.py",
            "resfs/src/resfs/file/py/foo/bar/ns.py.yapyc3=lib/ns.py.yapyc3",
            "resfs/src/resfs/file/py/foo/bar/pkg/mod.py=lib/pkg/mod.py",
            "resfs/src/resfs/file/py/foo/bar/pkg/mod.py.yapyc3=lib/pkg/mod.py.zsw2.yapyc3",
        ])
        objcopy_with_kv(graph, "lib", "resfs/src/resfs/file/py/lib/plain.py=lib/plain.py")
        stub = objcopy_with_kv(
            graph, "lib", "resfs/src/resfs/file/py/foo/bar/stub.pyi=lib/stub.pyi")
        self.assertIn("$(S)/lib/stub.pyi", stub["inputs"])

        namespaces = sorted(
            kv.rsplit("/", 1)[-1]
            for node in objcopy_nodes(graph, "lib")
            for kv in kvs(node)
            if kv.startswith("py/namespace/")
        )
        self.assertEqual(namespaces, ["lib=.", "lib=foo.bar.", "lib=lib."])
        archive = lib.node_by_output(graph, "$(B)/lib/libpy3lib.global.a")
        for node in objcopy_nodes(graph, "lib"):
            self.assertIn(node["outputs"][0], archive["inputs"])

    def test_kv_only_resources(self):
        files = python_base()
        files.update({
            "lib/ya.make": (
                "PY3_LIBRARY()\n"
                "PY_SRCS(plain.py)\n"
                "NO_CHECK_IMPORTS(foo.* bar.baz)\n"
                "PY_MAIN(lib/plain)\n"
                "YA_CONF_JSON(conf/ya.conf.json)\n"
                "END()\n"
            ),
            "lib/plain.py": "",
            "conf/ya.conf.json": (
                '{"a": {"formula": "build/f.json"}, "b": {"formula": "build/f.json"},\n'
                ' "x": {"resource": "R1"}, "y": {"resource": "R2"}, "z": {"resource": "R1"}}\n'
            ),
            "build/f.json": "{}\n",
            "build/external_resources/R1/resources.json": "{}\n",
        })
        graph = lib.make(files, "lib")
        objcopy_with_kv(graph, "lib", "PY_MAIN=lib.plain:main")
        objcopy_with_kv(
            graph, "lib", "py/no_check_imports/f23z6cptz7bgtuua63d4oosf2u=foo.* bar.baz")

        conf = objcopy_with_kv(
            graph, "lib", "resfs/src/resfs/file/ya.conf.json=conf/ya.conf.json")
        self.assertIn("$(S)/conf/ya.conf.json", conf["inputs"])
        self.assertEqual(keys(conf), ["resfs/file/ya.conf.json"])
        formula = objcopy_with_kv(
            graph, "lib", "resfs/src/resfs/file/build/f.json=build/f.json")
        self.assertIn("$(S)/build/f.json", formula["inputs"])
        resource = objcopy_with_kv(
            graph, "lib",
            "resfs/src/resfs/file/build/external_resources/R1/resources.json"
            "=build/external_resources/R1/resources.json",
        )
        self.assertIn("$(S)/build/external_resources/R1/resources.json", resource["inputs"])
        all_kvs = [kv for node in objcopy_nodes(graph, "lib") for kv in kvs(node)]
        self.assertFalse([kv for kv in all_kvs if "R2" in kv])
        self.assertEqual(sum("build/f.json" in kv for kv in all_kvs), 1)
        self.assertEqual(sum("R1/resources.json" in kv for kv in all_kvs), 1)

    def test_py_register_generates_and_compiles_registration(self):
        files = python_base()
        files.update({
            "lib/ya.make": (
                "PY3_LIBRARY()\n"
                "PY_REGISTER(my.module other)\n"
                "CFLAGS(-DPyInit_foreign=PyInit_x -DPyInit_bare -DKEEP)\n"
                "END()\n"
            ),
        })
        graph = lib.make(files, "lib")
        reg = lib.node_by_output(graph, "$(B)/lib/my.module.reg3.cpp")
        self.assertEqual(reg["kv"]["p"], "PY")
        self.assertEqual(
            reg["cmds"][0]["cmd_args"][1:],
            ["$(S)/build/scripts/gen_py3_reg.py", "my.module", "$(B)/lib/my.module.reg3.cpp"],
        )
        self.assertEqual(reg["inputs"], ["$(S)/build/scripts/gen_py3_reg.py"])
        compile_node = lib.node_by_output(graph, "$(B)/lib/my.module.reg3.cpp.o")
        self.assertIn("$(B)/lib/my.module.reg3.cpp", compile_node["inputs"])
        self.assertIn(reg["uid"], compile_node["deps"])
        args = compile_node["cmds"][0]["cmd_args"]
        self.assertIn("-DKEEP", args)
        self.assertNotIn("-DPyInit_foreign=PyInit_x", args)
        self.assertNotIn("-DPyInit_bare", args)
        self.assertNotIn("-DPyInit_module=PyInit_2my6module", args)

        second = lib.node_by_output(graph, "$(B)/lib/other.reg3.cpp.o")
        second_args = second["cmds"][0]["cmd_args"]
        self.assertIn("-DPyInit_module=PyInit_2my6module", second_args)
        self.assertIn("-Dinit_module_module=init_module_2my6module", second_args)
        archive = lib.node_by_output(graph, "$(B)/lib/libpy3lib.global.a")
        self.assertIn("$(B)/lib/my.module.reg3.cpp.o", archive["inputs"])
        self.assertIn("$(B)/lib/other.reg3.cpp.o", archive["inputs"])

    def test_generated_and_relocated_sources(self):
        files = python_base()
        files.update({
            "lib/ya.make": (
                "PY3_LIBRARY()\n"
                "RUN_PYTHON3(gen.py OUT gen_out.py)\n"
                "COPY_FILE(orig.py copied.py)\n"
                "SRCDIR(other)\n"
                "PY_SRCS(gen_out.py copied.py sub/in_other.py top/rootfile.py nowhere.py)\n"
                "PY_SRCS(NAMESPACE only.generated gen_out.py)\n"
                "END()\n"
            ),
            "lib/gen.py": "",
            "lib/orig.py": "",
            "other/sub/in_other.py": "",
            "top/rootfile.py": "",
        })
        graph = lib.make(files, "lib")

        producer = lib.node_by_output(graph, "$(B)/lib/gen_out.py")
        generated = lib.node_by_output(graph, "$(B)/lib/gen_out.py.yapyc3")
        self.assertEqual(
            cmd(generated),
            f"{PY3CC} gen_out.py- $(B)/lib/gen_out.py $(B)/lib/gen_out.py.yapyc3",
        )
        self.assertEqual(generated["inputs"], [
            "$(B)/tools/py3cc/py3cc", "$(B)/tools/py3cc/slow/slow",
            "$(B)/lib/gen_out.py", "$(S)/lib/gen.py",
        ])
        self.assertIn(producer["uid"], generated["deps"])
        copied = lib.node_by_output(graph, "$(B)/lib/copied.py.yapyc3")
        self.assertIn("$(S)/lib/orig.py", copied["inputs"])
        relocated = lib.node_by_output(graph, "$(B)/lib/sub/in_other.py.zsw2.yapyc3")
        self.assertEqual(
            cmd(relocated),
            f"{PY3CC} other/sub/in_other.py- $(S)/other/sub/in_other.py "
            "$(B)/lib/sub/in_other.py.zsw2.yapyc3",
        )

        resources = objcopy_with_kv(
            graph, "lib", "resfs/src/resfs/file/py/lib/gen_out.py=lib/gen_out.py")
        self.assertEqual(kvs(resources), [
            "resfs/src/resfs/file/py/lib/gen_out.py=lib/gen_out.py",
            "resfs/src/resfs/file/py/lib/gen_out.py.yapyc3=lib/gen_out.py.yapyc3",
            "resfs/src/resfs/file/py/lib/copied.py=lib/copied.py",
            "resfs/src/resfs/file/py/lib/copied.py.yapyc3=lib/copied.py.yapyc3",
            "resfs/src/resfs/file/py/lib/sub/in_other.py=other/sub/in_other.py",
            "resfs/src/resfs/file/py/lib/sub/in_other.py.yapyc3"
            "=lib/sub/in_other.py.zsw2.yapyc3",
            "resfs/src/resfs/file/py/lib/top/rootfile.py=top/rootfile.py",
            "resfs/src/resfs/file/py/lib/top/rootfile.py.yapyc3"
            "=lib/top/rootfile.py.zsw2.yapyc3",
            "resfs/src/resfs/file/py/lib/nowhere.py=lib/nowhere.py",
            "resfs/src/resfs/file/py/lib/nowhere.py.yapyc3=lib/nowhere.py.yapyc3",
        ])
        for expected in (
            "$(S)/lib/orig.py", "$(B)/lib/gen_out.py", "$(B)/lib/copied.py",
            "$(S)/other/sub/in_other.py", "$(S)/top/rootfile.py", "$(S)/lib/nowhere.py",
        ):
            self.assertIn(expected, resources["inputs"])
        objcopy_with_kv(
            graph, "lib", "resfs/src/resfs/file/py/only/generated/gen_out.py=lib/gen_out.py")

        namespaces = sorted(
            kv.split("/", 3)[-1]
            for node in objcopy_nodes(graph, "lib")
            for kv in kvs(node)
            if kv.startswith("py/namespace/")
        )
        self.assertEqual(namespaces, ["=lib.", "lib=lib.", "other=lib."])

    def test_parent_relative_source_compiles_resolved_file(self):
        files = python_base()
        files.update({
            "lib/ya.make": (
                "PY3_LIBRARY()\nPY_SRCS(../sibling/s.py)\n"
                "SRCDIR(deep/other)\nPY_SRCS(../sibling/t.py)\nEND()\n"
            ),
            "sibling/s.py": "",
            "deep/sibling/t.py": "",
            "deep/other/keep.txt": "",
        })
        graph = lib.make(files, "lib")
        for name, source in (("s.py", "sibling/s.py"), ("t.py", "deep/sibling/t.py")):
            compiled = [
                node for node in graph["graph"]
                if node["kv"].get("p") == "PY"
                and node["outputs"][0].endswith(f"/{name}.zsw2.yapyc3")
            ]
            self.assertEqual(len(compiled), 1)
            self.assertEqual(compiled[0]["inputs"][-1], f"$(S)/{source}")
            resources = [
                node for node in objcopy_nodes(graph, "lib")
                if compiled[0]["outputs"][0] in node["inputs"]
            ]
            self.assertEqual(len(resources), 1)

    def test_build_root_source_is_packed_as_generated(self):
        files = python_base()
        files.update({
            "lib/ya.make": (
                "PY3_LIBRARY()\n"
                "RUN_PYTHON3(gen.py OUT gen_out.py)\n"
                "PY_SRCS(${ARCADIA_BUILD_ROOT}/lib/gen_out.py a.py)\n"
                "END()\n"
            ),
            "lib/gen.py": "",
            "lib/a.py": "",
        })
        graph = lib.make(files, "lib")
        pyc = lib.node_by_output(graph, "$(B)/lib/gen_out.py.yapyc3")
        self.assertEqual(
            cmd(pyc),
            f"{PY3CC} lib/gen_out.py- $(B)/lib/gen_out.py $(B)/lib/gen_out.py.yapyc3",
        )
        self.assertIn("$(S)/lib/gen.py", pyc["inputs"])
        packs = [
            node for node in graph["graph"]
            if node["kv"].get("p") == "PR" and node["outputs"][0].startswith("$(B)/lib/")
        ]
        self.assertEqual(len(packs), 1)
        self.assertIn(
            "resfs/src/resfs/file/py/lib/gen_out.py.yapyc3=lib/gen_out.py.yapyc3",
            packs[0]["cmds"][0]["cmd_args"],
        )
        aux = lib.node_by_output(graph, packs[0]["outputs"][0] + ".o")
        self.assertIn("$(S)/lib/gen.py", aux["inputs"])
        source = objcopy_with_kv(
            graph, "lib", "resfs/src/resfs/file/py/lib/gen_out.py=lib/gen_out.py")
        self.assertIn("$(B)/lib/gen_out.py", source["inputs"])
        archive = lib.node_by_output(graph, "$(B)/lib/libpy3lib.global.a")
        self.assertIn(aux["outputs"][0], archive["inputs"])

    def test_pybuild_switches_select_source_or_bytecode(self):
        files = python_base()
        files.update({
            "nopyc/ya.make": (
                "PY3_LIBRARY()\nENABLE(PYBUILD_NO_PYC)\nPY_SRCS(a.py)\nEND()\n"
            ),
            "nopyc/a.py": "",
            "nopy/ya.make": (
                "PY3_LIBRARY()\nENABLE(PYBUILD_NO_PY)\nPY_SRCS(a.py)\nEND()\n"
            ),
            "nopy/a.py": "",
        })
        graph = lib.make(files, "nopyc")
        self.assertFalse([n for n in graph["graph"] if n["outputs"][0].endswith(".yapyc3")])
        res = objcopy_with_kv(graph, "nopyc", "resfs/src/resfs/file/py/nopyc/a.py=nopyc/a.py")
        self.assertEqual(keys(res), ["resfs/file/py/nopyc/a.py"])

        graph = lib.make(files, "nopy")
        res = objcopy_with_kv(
            graph, "nopy", "resfs/src/resfs/file/py/nopy/a.py.yapyc3=nopy/a.py.yapyc3")
        self.assertEqual(keys(res), ["resfs/file/py/nopy/a.py.yapyc3"])
        self.assertIn("$(B)/nopy/a.py.yapyc3", res["inputs"])

    def test_contrib_python_and_disabled_outputs_emit_no_source_objects(self):
        files = python_base()
        files.update({
            "contrib/python/pkg/ya.make": (
                "PY3_LIBRARY()\nENABLE(PYBUILD_NO_PY PYBUILD_NO_PYC)\nPY_SRCS(a.py)\nEND()\n"
            ),
            "contrib/python/pkg/a.py": "",
            "contrib/python/other/ya.make": "PY3_LIBRARY()\nPY_SRCS(b.py)\nEND()\n",
            "contrib/python/other/b.py": "",
            "plain/ya.make": "LIBRARY()\nPY_SRCS(c.py)\nEND()\n",
            "plain/c.py": "",
        })
        graph = lib.make(files, "contrib/python/pkg")
        self.assertEqual(objcopy_nodes(graph, "contrib/python/pkg"), [])

        graph = lib.make(files, "contrib/python/other")
        all_kvs = [kv for node in objcopy_nodes(graph, "contrib/python/other") for kv in kvs(node)]
        self.assertEqual(all_kvs, [
            "resfs/src/resfs/file/py/contrib/python/other/b.py=contrib/python/other/b.py",
            "resfs/src/resfs/file/py/contrib/python/other/b.py.yapyc3"
            "=contrib/python/other/b.py.yapyc3",
        ])

        graph = lib.make(files, "plain")
        lib.node_by_output(graph, "$(B)/plain/c.py.yapyc3")
        self.assertEqual(objcopy_nodes(graph, "plain"), [])

    def test_no_extended_source_search_skips_namespace(self):
        files = python_base()
        files.update({
            "lib/ya.make": (
                "PY3_LIBRARY()\nNO_EXTENDED_SOURCE_SEARCH()\nPY_SRCS(a.py)\n"
                "PY_SRCS(NAMESPACE x b.proto)\nEND()\n"
            ),
            "lib/a.py": "",
            "lib/b.proto": 'syntax = "proto3";\n',
        })
        lib.tool_program(files, "contrib/tools/protoc", "protoc")
        lib.tool_program(files, "contrib/python/mypy-protobuf/bin/protoc-gen-mypy", "protoc-gen-mypy")
        files["contrib/python/protobuf/ya.make"] = STUB_LIBRARY
        files["build/scripts/gen_py_protos.py"] = ""
        graph = lib.make(files, "lib")
        all_kvs = [kv for node in objcopy_nodes(graph, "lib") for kv in kvs(node)]
        self.assertFalse([kv for kv in all_kvs if kv.startswith("py/namespace/")])

    def test_all_py_srcs_and_program_main(self):
        files = python_base()
        files.update({
            "app/ya.make": (
                "PY3_PROGRAM()\n"
                "ALL_PY_SRCS(RECURSIVE NO_TEST_FILES)\n"
                "PY_SRCS(__main__.py)\n"
                "END()\n"
            ),
            "app/__main__.py": "",
            "app/mod.py": "",
            "app/test_mod.py": "",
            "app/pkg/inner.py": "",
            "app/pkg/inner_test.py": "",
        })
        graph = lib.make(files, "app")
        main = objcopy_with_kv(graph, "app", "PY_MAIN=app.__main__")
        link = lib.node_by_output(graph, "$(B)/app/app")
        self.assertIn(main["outputs"][0], link["inputs"])
        compiled = sorted(
            node["outputs"][0] for node in graph["graph"]
            if node["outputs"][0].endswith(".yapyc3")
        )
        self.assertEqual(compiled, [
            "$(B)/app/__main__.py.yapyc3",
            "$(B)/app/mod.py.yapyc3",
            "$(B)/app/pkg/inner.py.qgor.yapyc3",
        ])

    def test_library_without_sources_keeps_kv_resources(self):
        files = python_base()
        files.update({
            "lib/ya.make": "PY3_LIBRARY()\nPY_MAIN(lib.cli:run)\nEND()\n",
        })
        graph = lib.make(files, "lib")
        nodes = objcopy_nodes(graph, "lib")
        self.assertEqual(len(nodes), 1)
        self.assertEqual(kvs(nodes[0]), ["PY_MAIN=lib.cli:run"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
