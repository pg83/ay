import unittest

from tst import lib
from tst.make.test_python2 import CYTHON_HEADERS as PY2_HEADERS, CYTHON_UTILITY as PY2_UTILITY, python2_base
from tst.make.test_python_cython import CYTHON_UTILITY, args, python_base


MODULE = "amber/extensions"
SOURCE = "branch/widget.pyx"


def fixture(version, mode, native=False, direct=False, noresolve=False):
    files = python_base()
    if version == 2:
        files.update(python2_base())
        files["contrib/tools/cython_py2/cython.py"] = ""
        files.update({"contrib/tools/cython_py2/Cython/Utility/" + name: "" for name in PY2_UTILITY})
        files.update({name: "" for name in PY2_HEADERS})
    public = mode.endswith("_H")
    c_mode = mode.startswith("CYTHON_C") and not mode.startswith("CYTHON_CPP")
    suffix = ".py2" if version == 2 else ""
    stem = "branch/widget" + suffix if public else SOURCE + suffix
    generated = stem + (".c" if c_mode else ".cpp")
    outputs = [f"$(B)/{MODULE}/{generated}"]
    if public:
        outputs.append(f"$(B)/{MODULE}/{stem}.h")
        if "_API_" in mode:
            outputs.append(f"$(B)/{MODULE}/{stem}_api.h")
    call = f"PY_SRCS({mode} {SOURCE}=public.widget)\n"
    if native:
        call = f"SRCS({SOURCE})\n"
    if direct:
        call = f"BUILDWITH_{mode}({SOURCE})\n"
    files[f"{MODULE}/ya.make"] = (
        f"PY{version}_LIBRARY(bundle)\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\n"
        "ADDINCL(${BINDIR} native FOR cython typed)\n"
        + ("SET(DONT_RESOLVE_INCLUDES yes)\n" if noresolve else "")
        + call + ("SRCS(consumer.c)\n" if public else "") + "END()\n"
    )
    files[f"{MODULE}/{SOURCE}"] = (
        'from bridge cimport value\ninclude "piece.pxi"\n'
        'cdef extern from "external.h":\n    pass\n'
    )
    files[f"{MODULE}/branch/piece.pxi"] = "cdef public int value\n"
    files["typed/bridge.pxd"] = "cdef int value\n"
    files["native/external.h"] = ""
    if public:
        files[f"{MODULE}/consumer.c"] = '#include "' + outputs[-1].split(MODULE + "/", 1)[1] + '"\n'
    return files, generated, outputs


class CythonPublicOutputsTest(unittest.TestCase):
    def assert_plan(self, graph, version, mode, generated, outputs, options=True, noresolve=False):
        node = lib.node_by_output(graph, outputs[0])
        self.assertEqual(node["outputs"], outputs)
        tool = "cython_py2" if version == 2 else "cython"
        script = f"$(S)/contrib/tools/{tool}/cython.py"
        c_mode = generated.endswith(".c")
        expected = [script, "-X", "legacy_implicit_noexcept=True", "-E", "UNAME_SYSNAME=Linux"]
        if options:
            expected += ["--module-name", "public.widget", "--init-suffix", "6public6widget",
                         "--source-root", "$(S)", "-X", f"set_initial_path={MODULE}/{SOURCE}"]
        expected += ([] if c_mode else ["--cplus"])
        expected += ["-I$(B)", "-I$(S)", "-I$(S)/typed",
                     f"-I$(S)/contrib/tools/{tool}/Cython/Includes",
                     f"$(S)/{MODULE}/{SOURCE}", "-o", outputs[0]]
        self.assertEqual(args(node)[1:], expected)
        self.assertEqual(node["env"], {"ARCADIA_ROOT_DISTBUILD": "$(S)"})
        utility = PY2_UTILITY if version == 2 else CYTHON_UTILITY
        inputs = [script, *(f"$(S)/contrib/tools/{tool}/Cython/Utility/{name}" for name in utility),
                  f"$(S)/{MODULE}/{SOURCE}"]
        if not noresolve:
            inputs += ["$(S)/typed/bridge.pxd", f"$(S)/{MODULE}/branch/piece.pxi"]
        self.assertCountEqual(node["inputs"], inputs)
        [compiled] = [n for n in graph["graph"] if n["kv"].get("p") == "CC"
                      and args(n)[-1] == outputs[0]]
        self.assertIn(node["uid"], compiled["deps"])
        self.assertIn(outputs[0], compiled["inputs"])
        if not noresolve:
            self.assertIn("$(S)/native/external.h", compiled["inputs"])
        for name in ("rescompiler", "rescompressor"):
            self.assertNotIn(f"$(B)/tools/{name}/{name}", node["inputs"])
        archive = lib.node_by_output(graph, f"$(B)/{MODULE}/libpy{'3' if version == 3 else ''}bundle.a")
        self.assertIn(compiled["uid"], archive["deps"])
        self.assertIn(compiled["outputs"][0], archive["inputs"])
        if len(outputs) > 1 and not noresolve:
            consumer = lib.node_by_output(graph, f"$(B)/{MODULE}/consumer.c.o")
            self.assertIn(outputs[-1], consumer["inputs"])
            self.assertIn(outputs[0], consumer["inputs"])
            self.assertIn(node["uid"], consumer["deps"])
            self.assertIn(consumer["uid"], archive["deps"])
        return node

    def test_python_versions_and_public_api_modes(self):
        for version in (2, 3):
            for mode in ("CYTHON_C", "CYTHON_CPP", "CYTHON_C_H", "CYTHON_CPP_H",
                         "CYTHON_C_API_H", "CYTHON_CPP_API_H"):
                with self.subTest(version=version, mode=mode):
                    files, generated, outputs = fixture(version, mode)
                    self.assert_plan(lib.make(files, MODULE), version, mode, generated, outputs)

    def test_native_src_and_direct_build_macros_keep_their_source_extension(self):
        for version in (2, 3):
            for mode, native in (("CYTHON_CPP", True), ("CYTHON_C", False), ("CYTHON_CPP", False)):
                with self.subTest(version=version, mode=mode, native=native):
                    files, generated, outputs = fixture(version, mode, native=native, direct=not native)
                    self.assert_plan(lib.make(files, MODULE), version, mode, generated, outputs, options=False)

    def test_no_resolve_keeps_versioned_outputs_and_tool_inputs(self):
        for mode in ("CYTHON_C_H", "CYTHON_CPP_API_H"):
            with self.subTest(mode=mode):
                files, generated, outputs = fixture(2, mode, noresolve=True)
                # NoResolve also applies to the native consumer's #include.
                files[f"{MODULE}/ya.make"] = files[f"{MODULE}/ya.make"].replace("SRCS(consumer.c)\n", "")
                graph = lib.make(files, MODULE)
                self.assert_plan(graph, 2, mode, generated, outputs, noresolve=True)

    def test_public_stems_ignore_py23_object_suffixes(self):
        for mode in ("CYTHON_C_H", "CYTHON_CPP_H", "CYTHON_C_API_H", "CYTHON_CPP_API_H"):
            with self.subTest(mode=mode):
                files, _, _ = fixture(2, mode)
                files[f"{MODULE}/ya.make"] = files[f"{MODULE}/ya.make"].replace(
                    "PY2_LIBRARY(bundle)", "PY23_LIBRARY(bundle)",
                ).replace("SRCS(consumer.c)\n", "")
                for version in (2, 3):
                    files[f"clients/v{version}/ya.make"] = (
                        f"PY{version}_PROGRAM(runner)\n{lib.BARE_MODULE}"
                        f"NO_PYTHON_INCLUDES()\nPEERDIR({MODULE})\nEND()\n"
                    )
                graph = lib.make(files, "clients/v2", "clients/v3")
                for version in (2, 3):
                    _, _, outputs = fixture(version, mode)
                    node = lib.node_by_output(graph, outputs[0])
                    self.assertEqual(node["outputs"], outputs)
                    self.assertEqual(args(node)[-1], outputs[0])
                    [compiled] = [n for n in graph["graph"] if n["kv"].get("p") == "CC"
                                  and args(n)[-1] == outputs[0]]
                    self.assertTrue(compiled["outputs"][0].endswith(f".py{version}.o"))
                    self.assertIn(node["uid"], compiled["deps"])

    def test_cpp_public_and_api_groups_follow_pinned_macro_dispatch_order(self):
        for version in (2, 3):
            with self.subTest(version=version):
                files, _, _ = fixture(version, "CYTHON_CPP_API_H")
                files[f"{MODULE}/ya.make"] = files[f"{MODULE}/ya.make"].replace(
                    f"PY_SRCS(CYTHON_CPP_API_H {SOURCE}=public.widget)",
                    "PY_SRCS(CYTHON_CPP_API_H api.pyx=public.api CYTHON_CPP_H public.pyx=public.header)",
                ).replace("SRCS(consumer.c)\n", "")
                files[f"{MODULE}/api.pyx"] = "cdef api int api_value\n"
                files[f"{MODULE}/public.pyx"] = "cdef public int value\n"
                graph = lib.make(files, MODULE)
                nodes = [n for n in graph["graph"] if n["kv"].get("p") == "CY"]
                suffix = ".py2" if version == 2 else ""
                self.assertEqual([n["outputs"] for n in nodes], [
                    [f"$(B)/{MODULE}/public{suffix}.cpp", f"$(B)/{MODULE}/public{suffix}.h"],
                    [f"$(B)/{MODULE}/api{suffix}.cpp", f"$(B)/{MODULE}/api{suffix}.h",
                     f"$(B)/{MODULE}/api{suffix}_api.h"],
                ])


if __name__ == "__main__":
    unittest.main(verbosity=2)
