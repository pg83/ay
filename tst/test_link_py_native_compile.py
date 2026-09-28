import unittest

import lib


NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
TOOLS = [
    "contrib/tools/protoc",
    "tools/py3cc",
    "tools/py3cc/slow",
    "tools/rescompiler",
]
LIBRARIES = [
    "contrib/libs/python",
    "contrib/tools/python3/Modules/_sqlite",
    "library/cpp/malloc/jemalloc",
    "library/cpp/resource",
    "library/python/import_tracing/constructor",
    "library/python/runtime_py3",
    "library/python/runtime_py3/main",
    "library/python/testing/import_test",
]
CYTHON_UTILITY = (
    "AsyncGen.c Buffer.c Builtins.c CConvert.pyx CMath.c CommonStructures.c "
    "CommonTypes.c Complex.c Coroutine.c CpdefEnums.pyx CppConvert.pyx "
    "CppSupport.cpp CythonFunction.c Dataclasses.c Embed.c Exceptions.c "
    "ExtensionTypes.c FunctionArguments.c ImportExport.c MemoryView.pyx "
    "MemoryView_C.c ModuleSetupCode.c NumpyImportArray.c ObjectHandling.c "
    "Optimize.c Overflow.c Printing.c Profile.c StringTools.c "
    "TestCyUtilityLoader.pyx TestCythonScope.pyx TestUtilityLoader.c UFuncs_C.c "
    "arrayarray.h"
).split()
PLAIN_FILES = [
    "contrib/libs/cxxsupp/openmp/omp.h",
    "contrib/tools/cython/cython.py",
    "contrib/tools/cython/generated_c_headers.h",
    "contrib/tools/cython/generated_cpp_headers.h",
] + [
    f"contrib/libs/python/Include/{name}"
    for name in (
        "Python.h", "compile.h", "frameobject.h", "longintrepr.h", "pyconfig.h",
        "pythread.h", "structmember.h", "traceback.h",
    )
] + [f"contrib/tools/cython/Cython/Utility/{name}" for name in CYTHON_UTILITY]
NUMPY_INCLUDE = "-I$(S)/contrib/python/numpy/include/numpy/core/include"


def fixture():
    files = {
        "r/ya.make": (
            f"PY3_LIBRARY()\n{NO_PLATFORM}PY_REGISTER(foo)\nSRCS(foo.cpp)\n"
            "CFLAGS(-DOWN=1)\nEND()\n"
        ),
        "r/foo.cpp": "int foo;\n",
        "pyr/ya.make": f"PY3_PROGRAM()\n{NO_PLATFORM}PEERDIR(r)\nEND()\n",
        "c/ya.make": f"PY3_LIBRARY()\n{NO_PLATFORM}PY_SRCS(x.pyx)\nEND()\n",
        "c/x.pyx": "def f():\n    pass\n",
        "pyc/ya.make": f"PY3_PROGRAM()\n{NO_PLATFORM}PEERDIR(c)\nEND()\n",
    }
    for path in TOOLS:
        lib.tool_program(files, path, path.split("/")[-1])
    for path in LIBRARIES:
        files[f"{path}/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}END()\n"
    for path in PLAIN_FILES:
        files[path] = "\n"
    return files


def cc_args(graph, output):
    return lib.node_by_output(graph, output)["cmds"][0]["cmd_args"]


class PythonNativeCompileTest(unittest.TestCase):
    def test_py_register_source_compiles_with_module_cflags(self):
        graph = lib.make(fixture(), "pyr")
        generator = lib.node_by_output(graph, "$(B)/r/foo.reg3.cpp")
        registered = lib.node_by_output(graph, "$(B)/r/foo.reg3.cpp.o")
        self.assertEqual(registered["deps"], [generator["uid"]])
        self.assertEqual(registered["inputs"][0], "$(B)/r/foo.reg3.cpp")
        args = cc_args(graph, "$(B)/r/foo.reg3.cpp.o")
        self.assertIn("-DOWN=1", args)
        self.assertIn("-DUSE_PYTHON3", args)
        global_archive = lib.node_by_output(graph, "$(B)/r/libpy3r.global.a")
        self.assertIn("$(B)/r/foo.reg3.cpp.o", global_archive["inputs"])
        self.assertIn("$(B)/r/foo.cpp.o", lib.node_by_output(graph, "$(B)/r/libpy3r.a")["inputs"])

    def test_cython_sources_compile_with_numpy_includes(self):
        graph = lib.make(fixture(), "pyc")
        cython = lib.node_by_output(graph, "$(B)/c/x.pyx.cpp")
        self.assertEqual(cython["kv"]["p"], "CY")
        compiled = lib.node_by_output(graph, "$(B)/c/x.pyx.cpp.o")
        self.assertEqual(compiled["deps"], [cython["uid"]])
        self.assertIn("$(S)/c/x.pyx", compiled["inputs"])
        for output in ("$(B)/c/x.pyx.cpp.o", "$(B)/c/c.x.reg3.cpp.o"):
            with self.subTest(output=output):
                self.assertIn(NUMPY_INCLUDE, cc_args(graph, output))


if __name__ == "__main__":
    unittest.main(verbosity=2)
