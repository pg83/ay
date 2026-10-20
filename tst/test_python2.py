import base64
import hashlib
import unittest

import lib


STUB_LIBRARY = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(s.cpp)\nEND()\n"
NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"

PEER_STUBS = (
    "build/rules/py2_deprecation",
    "contrib/deprecated/python/subprocess32",
    "contrib/tools/python/lib",
    "contrib/tools/python/src/Modules/_sqlite",
    "library/cpp/malloc/jemalloc",
    "library/cpp/resource",
    "library/python/import_tracing/constructor",
    "library/python/runtime",
    "library/python/runtime/main",
    "library/python/runtime_py3",
    "library/python/testing/import_test",
)

TOOLS = (
    "tools/archiver",
    "tools/py2cc",
    "tools/py2cc/stage0pycc",
    "tools/rescompiler",
    "tools/rescompressor",
)

CONTRIB_LIBS_PYTHON = (
    "PY23_LIBRARY()\n"
    "NO_PYTHON_INCLUDES()\n"
    "IF (MODULE_TAG == \"PY2\")\n"
    "    CFLAGS(GLOBAL -DUSE_PYTHON2)\n"
    "    PEERDIR(contrib/tools/python/lib library/python/runtime)\n"
    "ELSE()\n"
    "    CFLAGS(GLOBAL -DUSE_PYTHON3)\n"
    "    PEERDIR(library/python/runtime_py3)\n"
    "ENDIF()\n"
    "END()\n"
)

STAGE0 = "$(B)/tools/py2cc/stage0pycc/py2cc"

CYTHON_UTILITY = (
    "arrayarray.h", "AsyncGen.c", "Buffer.c", "Builtins.c", "CConvert.pyx", "CMath.c",
    "CommonStructures.c", "CommonTypes.c", "Complex.c", "Coroutine.c", "CpdefEnums.pyx",
    "CppConvert.pyx", "CppSupport.cpp", "CythonFunction.c", "Dataclasses.c", "Embed.c",
    "Exceptions.c", "ExtensionTypes.c", "FunctionArguments.c", "ImportExport.c",
    "MemoryView.pyx", "MemoryView_C.c", "ModuleSetupCode.c", "NumpyImportArray.c",
    "ObjectHandling.c", "Optimize.c", "Overflow.c", "Printing.c", "Profile.c",
    "StringTools.c", "TestCyUtilityLoader.pyx", "TestCythonScope.pyx",
    "TestUtilityLoader.c", "UFuncs_C.c",
)

CYTHON_HEADERS = (
    "contrib/tools/cython_py2/generated_c_headers.h",
    "contrib/tools/cython_py2/generated_cpp_headers.h",
    "contrib/libs/python/Include/compile.h",
    "contrib/libs/python/Include/frameobject.h",
    "contrib/libs/python/Include/longintrepr.h",
    "contrib/libs/python/Include/pyconfig.h",
    "contrib/libs/python/Include/Python.h",
    "contrib/libs/python/Include/pythread.h",
    "contrib/libs/python/Include/structmember.h",
    "contrib/libs/python/Include/traceback.h",
    "contrib/libs/cxxsupp/openmp/omp.h",
)


def python2_base():
    files = {f"{path}/ya.make": STUB_LIBRARY for path in PEER_STUBS}
    files.update({f"{path}/s.cpp": "int s;\n" for path in PEER_STUBS})
    for path in TOOLS:
        lib.tool_program(files, path, path.rsplit("/", 1)[-1] if path != "tools/py2cc/stage0pycc" else "py2cc")
    files["contrib/libs/python/ya.make"] = CONTRIB_LIBS_PYTHON
    return files


def args(node):
    return node["cmds"][0]["cmd_args"]


def section(node, flag):
    out = []
    rest = args(node)[args(node).index(flag) + 1:]
    for arg in rest:
        if arg.startswith("--"):
            break
        out.append(arg)
    return out


def keys(node):
    return [base64.b64decode(key).decode() for key in section(node, "--keys")]


def objcopy_nodes(graph, module):
    prefix = f"$(B)/{module}/objcopy_"
    return [
        node for node in graph["graph"]
        if any(out.startswith(prefix) for out in node["outputs"])
    ]


def objcopy_with_kv(graph, module, kv):
    nodes = [node for node in objcopy_nodes(graph, module) if kv in section(node, "--kvs")]
    if len(nodes) != 1:
        raise AssertionError(f"expected one objcopy with {kv!r}, got {len(nodes)}")
    return nodes[0]


def py2_resource_hash(module, items):
    parts = [f"$S/{module}"]
    for path, mod in items:
        parts += [
            path,
            base64.b64encode(f"/py_modules/{mod}".encode()).decode(),
            f'resfs/src//py_modules/{mod}=${{rootrel;context=TEXT;input=TEXT:"{path}"}}',
            f"{path}.yapyc",
            base64.b64encode(f"/py_code/{mod}".encode()).decode(),
        ]
    return hashlib.md5((",".join(sorted(parts)) + "PY2").encode()).hexdigest()[:26]


class Python2Test(unittest.TestCase):
    def test_py2_library_compiles_sources_with_py2cc_into_resources(self):
        files = python2_base()
        files.update({
            "lib/ya.make": (
                "PY2_LIBRARY()\n"
                "ENABLE(USE_LIGHT_PY2CC)\n"
                "PY_SRCS(TOP_LEVEL top.py)\n"
                "PY_SRCS(pkg/mod.py __init__.py)\n"
                "SRCS(x.cpp)\n"
                "END()\n"
            ),
            "lib/top.py": "",
            "lib/pkg/mod.py": "",
            "lib/__init__.py": "",
            "lib/x.cpp": "int x;\n",
            "prog/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(lib)\nEND()\n",
            "prog/m.cpp": "int main(){return 0;}\n",
        })
        graph = lib.make(files, "prog")

        top = lib.node_by_output(graph, "$(B)/lib/top.py.yapyc")
        self.assertEqual(top["kv"]["p"], "PY")
        self.assertEqual(args(top), [STAGE0, "lib/top.py-", "$(S)/lib/top.py", "$(B)/lib/top.py.yapyc"])
        self.assertEqual(top["inputs"], [STAGE0, "$(S)/lib/top.py"])
        self.assertEqual(top["cmds"][0]["env"]["PYTHONHASHSEED"], "0")
        nested = lib.node_by_output(graph, "$(B)/lib/pkg/mod.py.zsw2.yapyc")
        self.assertEqual(args(nested)[1:3], ["lib/pkg/mod.py-", "$(S)/lib/pkg/mod.py"])

        top_res = objcopy_with_kv(graph, "lib", "resfs/src//py_modules/top=lib/top.py")
        self.assertEqual(keys(top_res), ["/py_modules/top", "/py_code/top"])
        self.assertEqual(section(top_res, "--inputs"), ["$(S)/lib/top.py", "$(B)/lib/top.py.yapyc"])
        self.assertEqual(
            top_res["outputs"],
            [f"$(B)/lib/objcopy_{py2_resource_hash('lib', [('top.py', 'top')])}.o"],
        )
        group = objcopy_with_kv(graph, "lib", "resfs/src//py_modules/lib.pkg.mod=lib/pkg/mod.py")
        self.assertEqual(keys(group), [
            "/py_modules/lib.pkg.mod", "/py_code/lib.pkg.mod",
            "/py_modules/lib.__init__", "/py_code/lib.__init__",
        ])
        self.assertEqual(section(group, "--kvs"), [
            "resfs/src//py_modules/lib.pkg.mod=lib/pkg/mod.py",
            "resfs/src//py_modules/lib.__init__=lib/__init__.py",
        ])

        self.assertIn("$(B)/lib/x.cpp.o", lib.node_by_output(graph, "$(B)/lib/libpylib.a")["inputs"])
        global_archive = lib.node_by_output(graph, "$(B)/lib/libpylib.global.a")
        for node in objcopy_nodes(graph, "lib"):
            self.assertIn(node["outputs"][0], global_archive["inputs"])

        compile_args = args(lib.node_by_output(graph, "$(B)/lib/x.cpp.o"))
        self.assertIn("-DARCADIA_PYTHON_UNICODE_SIZE=4", compile_args)
        self.assertIn("-I$(S)/contrib/libs/python/Include", compile_args)
        self.assertIn("-DUSE_PYTHON2", compile_args)
        self.assertNotIn("-DUSE_PYTHON3", compile_args)

    def test_py_srcs_scope_directives_apply_to_the_following_sources(self):
        files = python2_base()
        files.update({
            "lib/ya.make": "PY2_LIBRARY()\nPY_SRCS(a.py TOP_LEVEL b.py NAMESPACE n c.py)\nEND()\n",
            "lib/a.py": "",
            "lib/b.py": "",
            "lib/c.py": "",
            "prog/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(lib)\nEND()\n",
            "prog/m.cpp": "int main(){return 0;}\n",
        })
        graph = lib.make(files, "prog")
        node = objcopy_with_kv(graph, "lib", "resfs/src//py_modules/lib.a=lib/a.py")
        self.assertEqual(section(node, "--kvs"), [
            "resfs/src//py_modules/lib.a=lib/a.py",
            "resfs/src//py_modules/b=lib/b.py",
            "resfs/src//py_modules/n.c=lib/c.py",
        ])

    def test_py2_cython_uses_the_py2_cython(self):
        files = python2_base()
        files.update({
            "lib/ya.make": f"PY2_LIBRARY()\n{NO_PLATFORM}PY_SRCS(TOP_LEVEL m.pyx)\nEND()\n",
            "lib/m.pyx": "",
            "prog/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(main.cpp)\nPEERDIR(lib)\nEND()\n",
            "prog/main.cpp": "int main(){return 0;}\n",
        })
        files["contrib/tools/cython_py2/cython.py"] = ""
        for name in CYTHON_UTILITY:
            files[f"contrib/tools/cython_py2/Cython/Utility/{name}"] = ""
        for path in CYTHON_HEADERS:
            files[path] = ""
        graph = lib.make(files, "prog")
        cy = lib.node_by_output(graph, "$(B)/lib/m.pyx.py2.cpp")
        self.assertEqual(cy["kv"]["p"], "CY")
        cy_args = args(cy)
        self.assertEqual(cy_args[1], "$(S)/contrib/tools/cython_py2/cython.py")
        self.assertIn("-I$(S)/contrib/tools/cython_py2/Cython/Includes", cy_args)
        self.assertNotIn("-I$(S)/contrib/tools/cython/Cython/Includes", cy_args)
        self.assertEqual(cy_args[-3:], ["$(S)/lib/m.pyx", "-o", "$(B)/lib/m.pyx.py2.cpp"])
        self.assertIn("$(S)/contrib/tools/cython_py2/Cython/Utility/Buffer.c", cy["inputs"])
        self.assertFalse([path for path in cy["inputs"] if "/contrib/tools/cython/" in path])
        compile_node = lib.node_by_output(graph, "$(B)/lib/m.pyx.py2.cpp.o")
        self.assertIn(cy["uid"], compile_node["deps"])
        lib.node_by_output(graph, "$(B)/lib/m.reg.cpp")

    def test_py2_bytecode_tool_follows_use_light_py2cc(self):
        files = python2_base()
        files.update({
            "lib/ya.make": "PY2_LIBRARY()\nPY_SRCS(a.py)\nEND()\n",
            "lib/a.py": "",
            "prog/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(lib)\nEND()\n",
            "prog/m.cpp": "int main(){return 0;}\n",
        })
        graph = lib.make(files, "prog")
        node = lib.node_by_output(graph, "$(B)/lib/a.py.yapyc")
        self.assertEqual(args(node)[0], "$(B)/tools/py2cc/py2cc")

    def test_py2_register_uses_the_py2_generator_and_init_defines(self):
        files = python2_base()
        files.update({
            "lib/ya.make": f"PY2_LIBRARY()\n{NO_PLATFORM}PY_REGISTER(my.module other)\nEND()\n",
            "prog/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(lib)\nEND()\n",
            "prog/m.cpp": "int main(){return 0;}\n",
        })
        graph = lib.make(files, "prog")
        reg = lib.node_by_output(graph, "$(B)/lib/my.module.reg.cpp")
        self.assertEqual(
            args(reg)[1:],
            ["$(S)/build/scripts/gen_py_reg.py", "my.module", "$(B)/lib/my.module.reg.cpp"],
        )
        self.assertEqual(reg["inputs"], ["$(S)/build/scripts/gen_py_reg.py"])
        first = args(lib.node_by_output(graph, "$(B)/lib/my.module.reg.cpp.o"))
        self.assertNotIn("-Dinitmodule=init2my6module", first)
        second = args(lib.node_by_output(graph, "$(B)/lib/other.reg.cpp.o"))
        self.assertIn("-Dinitmodule=init2my6module", second)
        self.assertIn("-Dinit_module_module=init_module_2my6module", second)

    def test_py2_consumers_select_the_py2_variants_of_multimodules(self):
        files = python2_base()
        files.update({
            "p23/ya.make": "PY23_LIBRARY()\nNO_PYTHON_INCLUDES()\nSRCS(x.cpp)\nPY_REGISTER(p23.mod)\nEND()\n",
            "p23/x.cpp": "int x;\n",
            "n23/ya.make": "PY23_NATIVE_LIBRARY()\nSRCS(GLOBAL y.cpp)\nEND()\n",
            "n23/y.cpp": "int y;\n",
            "py2/ya.make": "PY2_LIBRARY()\nNO_PYTHON_INCLUDES()\nPEERDIR(p23 n23)\nEND()\n",
            "plain/ya.make": f"LIBRARY()\n{NO_PLATFORM}PEERDIR(p23 n23)\nEND()\n",
            "prog/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(py2 plain)\nEND()\n",
            "prog/m.cpp": "int main(){return 0;}\n",
        })
        graph = lib.make(files, "prog")

        py2 = lib.node_by_output(graph, "$(B)/p23/libpyp23.a")
        self.assertIn("$(B)/p23/x.cpp.py2.o", py2["inputs"])
        py2_args = args(lib.node_by_output(graph, "$(B)/p23/x.cpp.py2.o"))
        self.assertIn("-DARCADIA_PYTHON_UNICODE_SIZE=4", py2_args)
        self.assertIn("-Dinitmod=init3p233mod", py2_args)
        self.assertNotIn("-DUSE_PYTHON3", py2_args)
        lib.node_by_output(graph, "$(B)/p23/p23.mod.reg.cpp")
        py3_args = args(lib.node_by_output(graph, "$(B)/p23/x.cpp.py3.o"))
        self.assertIn("-DUSE_PYTHON3", py3_args)
        self.assertIn("-DPyInit_mod=PyInit_3p233mod", py3_args)
        lib.node_by_output(graph, "$(B)/p23/libpy3p23.a")

        native = lib.node_by_output(graph, "$(B)/n23/libn23.global.a")
        self.assertIn("$(B)/n23/y.cpp.py2.o", native["inputs"])
        self.assertIn("-DARCADIA_PYTHON_UNICODE_SIZE=4", args(lib.node_by_output(graph, "$(B)/n23/y.cpp.py2.o")))
        lib.node_by_output(graph, "$(B)/n23/libpy3cn23.global.a")

    def test_py2_variant_sees_the_py2_module_tag(self):
        files = python2_base()
        files.update({
            "lib/ya.make": f"PY2_LIBRARY()\n{NO_PLATFORM}SRCS(x.cpp)\nEND()\n",
            "lib/x.cpp": "int x;\n",
            "prog/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(lib)\nEND()\n",
            "prog/m.cpp": "int main(){return 0;}\n",
        })
        graph = lib.make(files, "prog")
        link = lib.node_by_output(graph, "$(B)/prog/prog")
        self.assertIn("$(B)/lib/libpylib.a", link["inputs"])
        self.assertIn("-DUSE_PYTHON2", args(lib.node_by_output(graph, "$(B)/lib/x.cpp.o")))
        self.assertFalse([node for node in graph["graph"] if "runtime_py3" in node["outputs"][0]])

    def test_py2_program_links_the_python2_runtime(self):
        files = python2_base()
        files.update({
            "prog/ya.make": (
                "PY2_PROGRAM(prog)\n"
                "NO_PYTHON_INCLUDES()\n"
                "ENABLE(USE_LIGHT_PY2CC)\n"
                "PY_SRCS(__main__.py)\n"
                "END()\n"
            ),
            "prog/__main__.py": "",
        })
        for library_peers, opensource in (
            (["build/rules/py2_deprecation"], False),
            ([], True),
        ):
            with self.subTest(opensource=opensource):
                graph = lib.make(files, "prog", "-r", opensource=opensource)
                link = lib.node_by_output(graph, "$(B)/prog/prog")
                peers = [
                    path.rsplit("/", 1)[0][len("$(B)/"):]
                    for path in link["inputs"]
                    if path.endswith(".a")
                ]
                expected = [
                    "library/cpp/malloc/jemalloc",
                    "library/python/runtime/main",
                    "contrib/deprecated/python/subprocess32",
                    "contrib/tools/python/src/Modules/_sqlite",
                    "library/python/import_tracing/constructor",
                ] + library_peers + [
                    "library/cpp/resource",
                    "library/python/testing/import_test",
                ]
                self.assertEqual([peer for peer in peers if peer in expected], expected)
                link_args = next(
                    cmd["cmd_args"] for cmd in link["cmds"]
                    if any(arg.endswith("link_exe.py") for arg in cmd["cmd_args"])
                )
                self.assertIn("-Wl,--strip-all", link_args)
                main = objcopy_with_kv(graph, "prog", "resfs/src//py_modules/__main__=prog/__main__.py")
                self.assertEqual(keys(main), ["/py_modules/__main__", "/py_code/__main__"])
                self.assertEqual(args(lib.node_by_output(graph, "$(B)/prog/__main__.py.yapyc"))[0], STAGE0)


if __name__ == "__main__":
    unittest.main(verbosity=2)
