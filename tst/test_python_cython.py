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
    "contrib/tools/cython/generated_c_headers.h",
    "contrib/tools/cython/generated_cpp_headers.h",
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

CYTHON = "$(S)/contrib/tools/cython/cython.py"
CYTHON_HEAD = [
    CYTHON, "-X", "legacy_implicit_noexcept=True", "-E", "UNAME_SYSNAME=Linux",
]


def python_base():
    files = {f"{path}/ya.make": STUB_LIBRARY for path in PEER_STUBS}
    for path in TOOLS:
        lib.tool_program(files, path, path.rsplit("/", 1)[-1])
    files["contrib/tools/cython/cython.py"] = ""
    for name in CYTHON_UTILITY:
        files[f"contrib/tools/cython/Cython/Utility/{name}"] = ""
    files["contrib/tools/cython/Cython/Utility/MemoryView.pxd"] = ""
    for path in CYTHON_HEADERS:
        files[path] = ""
    files["app/ya.make"] = (
        "PY3_PROGRAM()\n"
        "PY_SRCS(MAIN main.py)\n"
        "PEERDIR(lib)\n"
        "END()\n"
    )
    files["app/main.py"] = ""
    return files


def args(node):
    return node["cmds"][0]["cmd_args"]


def module_options(name, src):
    return [
        "--module-name", f"lib.{name}",
        "--init-suffix", f"3lib{len(name)}{name}",
        "--source-root", "$(S)",
        "-X", f"set_initial_path=lib/{src}",
    ]


class CythonTest(unittest.TestCase):
    def test_cython_system_addincl_reuses_existing_native_include_directories(self):
        paths = (
            "contrib/python/numpy/include/numpy/core/include",
            "contrib/python/numpy/include/numpy/core/include/numpy",
            "contrib/python/numpy/include/numpy/core/src/common",
            "contrib/python/numpy/include/numpy/core/src/npymath",
            "contrib/python/numpy/include/numpy/distutils/include",
        )
        for macro in ("BUILDWITH_CYTHON_C(fast.pyx)", "PY_SRCS(CYTHON_C fast.pyx)"):
            with self.subTest(macro=macro):
                files = python_base()
                files.update({
                    "lib/ya.make": "PY3_LIBRARY()\nADDINCL(" + " ".join(paths) + ")\n" + macro + "\nEND()\n",
                    "lib/fast.pyx": "",
                    **{path + "/.keep": "" for path in paths},
                })
                graph = lib.make(files, "app")
                compiled = next(node for node in graph["graph"]
                    if node["kv"].get("p") == "CC" and "$(B)/lib/fast.pyx.c" in node["inputs"])
                for path in paths:
                    self.assertEqual(args(compiled).count("-I$(S)/" + path), 1)

    def test_cython_flags_append_to_module_options_before_per_source_options(self):
        files = python_base()
        files.update({
            "lib/ya.make": "PY3_LIBRARY()\nBUILDWITH_CYTHON_CPP(a.pyx --module-name selected)\n"
                "CYTHON_FLAGS(-E FEATURE=1)\nCYTHON_FLAGS(-X boundscheck=False)\nEND()\n",
            "lib/a.pyx": "",
        })
        command = args(lib.node_by_output(lib.make(files, "app"), "$(B)/lib/a.pyx.cpp"))
        self.assertEqual(command[1:command.index("--cplus")], [
            *CYTHON_HEAD, "-E", "FEATURE=1", "-X", "boundscheck=False", "--module-name", "selected",
        ])

    def test_set_cython_options_replaces_configured_defaults(self):
        files = python_base()
        files.update({
            "lib/ya.make": "PY3_LIBRARY()\nSET(CYTHON_OPTIONS -X embedsignature=True)\n"
                "CYTHON_FLAGS(-E CUSTOM=1)\nBUILDWITH_CYTHON_C(a.pyx)\nEND()\n",
            "lib/a.pyx": "",
        })
        command = args(lib.node_by_output(lib.make(files, "app"), "$(B)/lib/a.pyx.c"))
        self.assertEqual(command[1:command.index("-I$(B)")], [CYTHON, "-X", "embedsignature=True", "-E", "CUSTOM=1"])
        self.assertNotIn("legacy_implicit_noexcept=True", command)

    def test_py_srcs_cython_variants(self):
        files = python_base()
        files.update({
            "lib/ya.make": (
                "PY23_LIBRARY()\n"
                "ADDINCL(FOR cython lib/cyinc)\n"
                "CFLAGS(-DKEEP=1)\n"
                "PY_SRCS(\n"
                "  CYTHON_DIRECTIVE language_level=3\n"
                "  a.py\n"
                "  fast.pyx\n"
                "  CYTHON_C cmod.pyx\n"
                "  CYTHON_CPP plain.pyx\n"
                "  CYTHON_CPP_H hdr_cpp.pyx\n"
                "  CYTHON_C_H hdr_c.pyx\n"
                "  CYTHON_C_API_H api.pyx\n"
                "  CYTHONIZE_PY cy.py\n"
                ")\n"
                "PY_REGISTER(explicit.mod)\n"
                "END()\n"
            ),
            "lib/a.py": "",
            "lib/fast.pyx": "",
            "lib/cmod.pyx": "",
            "lib/plain.pyx": "",
            "lib/hdr_cpp.pyx": "",
            "lib/hdr_c.pyx": "",
            "lib/api.pyx": "",
            "lib/cy.py": "",
            "lib/lib/cy.pxd": "cdef int f()\n",
            "lib/cyinc/keep.pxd": "",
        })
        graph = lib.make(files, "app")

        fast = lib.node_by_output(graph, "$(B)/lib/fast.pyx.py3.cpp")
        self.assertEqual(fast["kv"]["p"], "CY")
        self.assertEqual(args(fast)[1:], [
            *CYTHON_HEAD,
            *module_options("fast", "fast.pyx"),
            "-X", "language_level=3",
            "--cplus", "-I$(B)", "-I$(S)", "-I$(S)/lib/cyinc",
            "-I$(S)/contrib/tools/cython/Cython/Includes",
            "$(S)/lib/fast.pyx", "-o", "$(B)/lib/fast.pyx.py3.cpp",
        ])
        self.assertEqual(fast["inputs"][0], CYTHON)
        self.assertIn("$(S)/contrib/tools/cython/Cython/Utility/MemoryView.pyx", fast["inputs"])
        # The embedded utility files are TEXT inputs (python.conf
        # _UNWRAP_TEXT_INPUTS), never parsed: no sibling MemoryView.pxd, as
        # in every CY node of the devtools_yamaker and devtools_ya_bin references.
        self.assertNotIn("$(S)/contrib/tools/cython/Cython/Utility/MemoryView.pxd", fast["inputs"])
        self.assertIn("$(S)/lib/fast.pyx", fast["inputs"])
        fast_cc = lib.node_by_output(graph, "$(B)/lib/fast.pyx.py3.cpp.py3.o")
        self.assertIn("-Wno-implicit-fallthrough", args(fast_cc))
        self.assertIn("-DKEEP=1", args(fast_cc))
        self.assertNotIn("-DPyInit_fast=PyInit_3lib4fast", args(fast_cc))
        self.assertIn("-I$(S)/contrib/python/numpy/include/numpy/core/include", args(fast_cc))
        self.assertIn(fast["uid"], fast_cc["deps"])
        self.assertIn("$(S)/contrib/tools/cython/generated_cpp_headers.h", fast_cc["inputs"])

        cmod = lib.node_by_output(graph, "$(B)/lib/cmod.pyx.c")
        self.assertNotIn("--cplus", args(cmod))
        cmod_cc = lib.node_by_output(graph, "$(B)/lib/cmod.pyx.c.py3.o")
        self.assertNotIn("-Wno-implicit-fallthrough", args(cmod_cc))
        self.assertNotIn("$(S)/contrib/tools/cython/generated_cpp_headers.h", cmod_cc["inputs"])

        lib.node_by_output(graph, "$(B)/lib/plain.pyx.cpp.py3.o")

        hdr_cpp = lib.node_by_output(graph, "$(B)/lib/hdr_cpp.cpp")
        self.assertEqual(hdr_cpp["outputs"], ["$(B)/lib/hdr_cpp.cpp", "$(B)/lib/hdr_cpp.h"])
        self.assertIn("--cplus", args(hdr_cpp))
        hdr_cpp_cc = lib.node_by_output(graph, "$(B)/lib/hdr_cpp.cpp.py3.o")
        self.assertNotIn("-Wno-implicit-fallthrough", args(hdr_cpp_cc))
        hdr_c = lib.node_by_output(graph, "$(B)/lib/hdr_c.c")
        self.assertEqual(hdr_c["outputs"], ["$(B)/lib/hdr_c.c", "$(B)/lib/hdr_c.h"])
        api = lib.node_by_output(graph, "$(B)/lib/api.c")
        self.assertEqual(api["outputs"], ["$(B)/lib/api.c", "$(B)/lib/api.h", "$(B)/lib/api_api.h"])

        cy = lib.node_by_output(graph, "$(B)/lib/cy.c")
        self.assertEqual(args(cy)[-3:], ["$(S)/lib/cy.py", "-o", "$(B)/lib/cy.c"])
        self.assertIn("$(S)/lib/lib/cy.pxd", cy["inputs"])

        explicit = lib.node_by_output(graph, "$(B)/lib/explicit.mod.reg3.cpp.py3.o")
        self.assertNotIn("-DPyInit_fast=PyInit_3lib4fast", args(explicit))
        self.assertIn("-DKEEP=1", args(explicit))
        self.assertIn("-I$(S)/contrib/python/numpy/include/numpy/core/include", args(explicit))

        archive = lib.node_by_output(graph, "$(B)/lib/libpy3lib.a")
        self.assertEqual(archive["inputs"][:7], [
            "$(B)/lib/cmod.pyx.c.py3.o",
            "$(B)/lib/hdr_c.c.py3.o",
            "$(B)/lib/api.c.py3.o",
            "$(B)/lib/cy.c.py3.o",
            "$(B)/lib/fast.pyx.py3.cpp.py3.o",
            "$(B)/lib/plain.pyx.cpp.py3.o",
            "$(B)/lib/hdr_cpp.cpp.py3.o",
        ])

    def test_py_srcs_cython_objects_follow_macro_generated_sources(self):
        # PY_SRCS is a plugin (build/plugins/pybuild.py) run after the whole
        # ya.make: middlesearch search/split_bert archives
        # split_bert_versions_mapping.cpp.o, the GENERATE_ENUM_SERIALIZATION
        # object declared after PY_SRCS, then bert_versions.pyx.cpp.o.
        files = python_base()
        files.update({
            "lib/ya.make": (
                "LIBRARY()\nUSE_PYTHON3()\nSRCS(m.cpp)\nPY_SRCS(b.pyx)\n"
                "GENERATE_ENUM_SERIALIZATION(m.h)\nEND()\n"
            ),
            "lib/m.cpp": "",
            "lib/m.h": "",
            "lib/b.pyx": "",
            "util/generic/serialized_enum.h": "",
            "tools/enum_parser/enum_serialization_runtime/ya.make": STUB_LIBRARY,
            "library/python/runtime_py3/ya.make": STUB_LIBRARY,
        })
        lib.tool_program(files, "tools/enum_parser/enum_parser", "enum_parser")
        graph = lib.make(files, "lib")
        archive = lib.node_by_output_prefix(graph, "$(B)/lib/liblib")
        objects = [i.rsplit("/", 1)[1] for i in archive["inputs"] if i.endswith(".o")]
        self.assertEqual(objects, ["m.cpp.o", "m.h_serialized.cpp.o", "b.pyx.cpp.o"])

    def test_cimport_forms_reach_cython_inputs(self):
        files = python_base()
        files.update({
            "lib/ya.make": (
                "PY3_LIBRARY()\n"
                "PY_SRCS(mod.pyx pkg/rel.pyx CYTHONIZE_PY nopxd.py)\n"
                "END()\n"
            ),
            "lib/mod.pyx": (
                "# comment line\n"
                "\n"
                "include 'inc.pxi'\n"
                'include "lib/dq.pxi"\n'
                "include 'helper.py'\n"
                "include bare\n"
                'cdef extern from "lib/ext.h":\n'
                "    pass\n"
                "cdef extern from *:\n"
                "    pass\n"
                "cdef int x\n"
                "from lib.deps cimport (a, b as c,  # names\n"
                "from lib.pkg cimport sub\n"
                "from lib cimport mod2, *\n"
                "from cython.parallel cimport prange\n"
                "from lib.plain import thing\n"
                "from (\n"
                "cimport lib.single, lib.pkg.nested as n, , cython\n"
                "cimport lib.missing\n"
            ),
            "lib/mod.pxd": "",
            "lib/inc.pxi": "",
            "lib/helper.py": "cimport lib.frompy\n",
            "lib/frompy.pxd": "",
            "lib/nopxd.py": "",
            "lib/dq.pxi": "",
            "lib/ext.h": "",
            "lib/deps.pxd": "",
            "lib/pkg/__init__.pxd": "",
            "lib/pkg/sub.pxd": "",
            "lib/mod2.pxd": "",
            "lib/single.pxd": "",
            "lib/pkg/nested/__init__.pxd": "",
            "lib/pkg/rel.pyx": "from . cimport sub\nfrom .. cimport mod2\nfrom .sub cimport thing\n",
        })
        graph = lib.make(files, "lib")
        cy = lib.node_by_output(graph, "$(B)/lib/mod.pyx.cpp")
        for expected in (
            "$(S)/lib/mod.pyx", "$(S)/lib/mod.pxd", "$(S)/lib/inc.pxi", "$(S)/lib/dq.pxi",
            "$(S)/lib/helper.py",
            "$(S)/lib/deps.pxd", "$(S)/lib/pkg/__init__.pxd", "$(S)/lib/pkg/sub.pxd",
            "$(S)/lib/mod2.pxd", "$(S)/lib/single.pxd", "$(S)/lib/pkg/nested/__init__.pxd",
        ):
            self.assertIn(expected, cy["inputs"])
        self.assertNotIn("$(S)/lib/frompy.pxd", cy["inputs"])
        self.assertNotIn("$(S)/lib/ext.h", cy["inputs"])
        compiled = lib.node_by_output(graph, "$(B)/lib/mod.pyx.cpp.o")
        self.assertIn("$(S)/lib/ext.h", compiled["inputs"])

        nopxd = lib.node_by_output(graph, "$(B)/lib/nopxd.py.cpp")
        self.assertEqual(args(nopxd)[-3:], ["$(S)/lib/nopxd.py", "-o", "$(B)/lib/nopxd.py.cpp"])
        self.assertFalse([path for path in nopxd["inputs"] if path.endswith("nopxd.pxd")])

        rel = lib.node_by_output(graph, "$(B)/lib/pkg/rel.pyx.cpp")
        for expected in ("$(S)/lib/pkg/__init__.pxd", "$(S)/lib/pkg/sub.pxd", "$(S)/lib/mod2.pxd"):
            self.assertIn(expected, rel["inputs"])

    def test_buildwith_cython_and_header_consumers(self):
        files = python_base()
        files.update({
            "lib/ya.make": (
                "PY3_LIBRARY()\n"
                "BUILDWITH_CYTHON_CPP(wrap.pyx --module-name wrap)\n"
                "BUILDWITH_CYTHON_C(cwrap.pyx --module-name cwrap)\n"
                "PY_SRCS(CYTHON_CPP_H exported.pyx)\n"
                "SRCS(user.cpp)\n"
                "END()\n"
            ),
            "lib/wrap.pyx": 'cdef extern from "lib/exported.h":\n    pass\n',
            "lib/cwrap.pyx": "",
            "lib/exported.pyx": "cimport lib.shared\n",
            "lib/shared.pxd": "",
            "lib/user.cpp": '#include "lib/exported.h"\nint u(){return 0;}\n',
        })
        graph = lib.make(files, "app")
        wrap = lib.node_by_output(graph, "$(B)/lib/wrap.pyx.cpp")
        self.assertEqual(args(wrap)[1:], [
            *CYTHON_HEAD, "--module-name", "wrap", "--cplus", "-I$(B)", "-I$(S)",
            "-I$(S)/contrib/tools/cython/Cython/Includes",
            "$(S)/lib/wrap.pyx", "-o", "$(B)/lib/wrap.pyx.cpp",
        ])
        self.assertNotIn("$(S)/lib/exported.pyx", wrap["inputs"])
        self.assertNotIn("$(S)/lib/shared.pxd", wrap["inputs"])
        cwrap = lib.node_by_output(graph, "$(B)/lib/cwrap.pyx.c")
        self.assertNotIn("--cplus", args(cwrap))

        compiled_wrap = lib.node_by_output(graph, "$(B)/lib/wrap.pyx.cpp.o")
        self.assertNotIn("$(S)/lib/wrap.pyx", compiled_wrap["inputs"])
        self.assertNotIn("$(S)/contrib/tools/cython/cython.py", compiled_wrap["inputs"])
        self.assertIn("$(B)/lib/exported.h", compiled_wrap["inputs"])
        wrap_cc = args(compiled_wrap)
        python_include = wrap_cc.index("-I$(S)/contrib/libs/python/Include")
        numpy_include = wrap_cc.index("-I$(S)/contrib/python/numpy/include/numpy/core/include")
        self.assertLess(numpy_include, python_include)

        exported = lib.node_by_output(graph, "$(B)/lib/exported.cpp")
        self.assertEqual(exported["outputs"], ["$(B)/lib/exported.cpp", "$(B)/lib/exported.h"])
        user = lib.node_by_output(graph, "$(B)/lib/user.cpp.o")
        self.assertIn("$(B)/lib/exported.h", user["inputs"])
        self.assertNotIn("$(S)/lib/exported.pyx", user["inputs"])
        self.assertNotIn("$(S)/lib/shared.pxd", user["inputs"])
        self.assertIn(exported["uid"], user["deps"])

    def test_cythonized_python_sources_are_linted_like_plain_ones(self):
        files = python_base()
        files.update({
            "cy/ya.make": "PY3_LIBRARY()\nPY_SRCS(plain.py)\nPY_SRCS(CYTHONIZE_PY fast.py)\nEND()\n",
            "cy/plain.py": "",
            "cy/fast.py": "",
        })
        graph = lib.make(files, "cy", "-t", opensource=False)
        lint = lib.node_by_output(graph, "$(B)/cy/test-results/flake8/meta.json")["cmds"][0]["cmd_args"]
        self.assertEqual(lint[-2:], ["$(S)/cy/fast.py", "$(S)/cy/plain.py"])

    def test_buildwith_cython_without_cflags(self):
        files = python_base()
        files.update({
            "solo/ya.make": "PY3_LIBRARY()\nBUILDWITH_CYTHON_C(solo.pyx)\nEND()\n",
            "solo/solo.pyx": "",
        })
        files["app/ya.make"] = files["app/ya.make"].replace("PEERDIR(lib)", "PEERDIR(solo)")
        graph = lib.make(files, "app")
        solo = lib.node_by_output(graph, "$(B)/solo/solo.pyx.c")
        self.assertEqual(args(solo)[1:], [
            *CYTHON_HEAD, "-I$(B)", "-I$(S)",
            "-I$(S)/contrib/tools/cython/Cython/Includes",
            "$(S)/solo/solo.pyx", "-o", "$(B)/solo/solo.pyx.c",
        ])
        compiled = lib.node_by_output(graph, "$(B)/solo/solo.pyx.c.o")
        self.assertIn("-I$(S)/contrib/python/numpy/include/numpy/core/include", args(compiled))
        archive = lib.node_by_output(graph, "$(B)/solo/libpy3solo.a")
        self.assertEqual(archive["inputs"][0], "$(B)/solo/solo.pyx.c.o")


if __name__ == "__main__":
    unittest.main(verbosity=2)
