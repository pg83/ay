"""Python producer rules exercised through the graph-generating binary."""
import base64
import hashlib
import unittest

import lib
from test_link_py_native_compile import fixture as native_files
from test_proto_flatc import flatc_files
from test_python_swig import python_base as swig_files
from test_python2 import CYTHON_HEADERS, CYTHON_UTILITY, python2_base


class PythonProducerContextsTest(unittest.TestCase):
    def test_yql_source_fallback_lowers_native_sources_after_its_epilogue(self):
        files = native_files()
        files.update({
            "apps/materializer/ya.make": (
                f"YQL_PREBUILT_PROGRAM(materializer)\n{lib.BARE_MODULE}"
                "SET(PYTHON3 yes)\nSRCS(logic.pyx)\nEND()\n"
            ),
            "apps/materializer/logic.pyx": "cdef int value\n",
        })
        graph = lib.make(files, "apps/materializer", "-DBUILD_YQL_FROM_SOURCES=yes", opensource=False)
        generated = lib.node_by_output(graph, "$(B)/apps/materializer/logic.pyx.cpp")
        compiled = lib.node_by_output(graph, "$(B)/apps/materializer/logic.pyx.cpp.o")
        program = lib.node_by_output(graph, "$(B)/apps/materializer/materializer")
        self.assertIn(generated["uid"], compiled["deps"])
        self.assertIn(compiled["uid"], program["deps"])

    def test_native_cython_python_variable_selects_include_domains(self):
        for py3 in (False, True):
            with self.subTest(py3=py3):
                files = native_files()
                for name in (*CYTHON_HEADERS, *(f"contrib/tools/cython_py2/Cython/Utility/{name}" for name in CYTHON_UTILITY)):
                    files[name] = ""
                files.update({
                    "bindings/local/ya.make": (
                        f"LIBRARY()\n{lib.BARE_MODULE}SET(PYTHON3 {'yes' if py3 else 'no'})\n"
                        + "SRCS(local.pyx)\nEND()\n"
                    ),
                    "bindings/local/local.pyx": "cimport abi\n",
                    "contrib/tools/cython/Cython/Includes/abi.pxd": "",
                    "contrib/tools/cython_py2/Cython/Includes/abi.pxd": "",
                })
                graph = lib.make(files, "bindings/local")
                suffix = ".cpp" if py3 else ".py2.cpp"
                generated = lib.node_by_output(graph, "$(B)/bindings/local/local.pyx" + suffix)
                cython = "cython" if py3 else "cython_py2"
                self.assertIn(f"$(S)/contrib/tools/{cython}/cython.py", generated["cmds"][0]["cmd_args"])
                self.assertIn(f"$(S)/contrib/tools/{cython}/Cython/Includes/abi.pxd", generated["inputs"])

    def test_native_python_versions_keep_separate_commands_and_objects(self):
        files = native_files()
        files.update(python2_base())
        for name in (*CYTHON_HEADERS, *(f"contrib/tools/cython_py2/Cython/Utility/{name}" for name in CYTHON_UTILITY)):
            files[name] = ""
        files.update({
            "bindings/dual/ya.make": (
                f"PY23_NATIVE_LIBRARY(dispatch)\n{lib.BARE_MODULE}SRCS(same.pyx)\n"
                "IF(PYTHON2)\nCYTHON_FLAGS(-EEDITION=2)\nELSE()\nCYTHON_FLAGS(-EEDITION=3)\nENDIF()\nEND()\n"
            ),
            "bindings/dual/same.pyx": "",
            "apps/legacy/ya.make": f"PY2MODULE(legacy)\n{lib.BARE_MODULE}PEERDIR(bindings/dual)\nEND()\n",
        })
        graph = lib.make(files, "bindings/dual", "apps/legacy")
        for version, generated, archive in (
            (2, "same.pyx.py2.py2.cpp", "libdispatch.a"),
            (3, "same.pyx.py3.cpp", "libpy3cdispatch.a"),
        ):
            output = "$(B)/bindings/dual/" + generated
            producer = lib.node_by_output(graph, output)
            args = producer["cmds"][0]["cmd_args"]
            cython = "cython_py2" if version == 2 else "cython"
            self.assertIn(f"$(S)/contrib/tools/{cython}/cython.py", args)
            self.assertIn(f"-EEDITION={version}", args)
            self.assertNotIn(f"-EEDITION={5 - version}", args)
            suffix = ".py2.pic.o" if version == 2 else ".py3.o"
            compiled = lib.node_by_output(graph, output + suffix)
            self.assertIn(producer["uid"], compiled["deps"])
            self.assertIn(compiled["uid"], lib.node_by_output(graph, "$(B)/bindings/dual/" + archive)["deps"])

    def test_python2_swig_and_flatbuffers_use_python2_compilers_and_resources(self):
        for light in (False, True):
            with self.subTest(light=light):
                files = swig_files()
                files.update(python2_base())
                lib.tool_program(files, "contrib/deprecated/flatc", "flatc")
                files.update({
                    "bindings/legacy/ya.make": (
                        f"PY2_LIBRARY()\n{lib.BARE_MODULE}"
                        + ("ENABLE(USE_LIGHT_PY2CC)\n" if light else "")
                        + "PY_SRCS(NAMESPACE old.api wrap.swg record.fbs)\nEND()\n"
                    ),
                    "bindings/legacy/wrap.swg": "",
                    "bindings/legacy/record.fbs": "table Record {}\n",
                    "contrib/python/flatbuffers/ya.make": f"PY23_LIBRARY()\n{lib.BARE_MODULE}END()\n",
                    "build/scripts/tar_sources.py": "",
                    "build/scripts/compile_pysrc.py": "",
                })
                graph = lib.make(files, "bindings/legacy")
                swig = lib.node_by_output(graph, "$(B)/bindings/legacy/wrap.swg.cpp")
                bytecode = lib.node_by_output_prefix(graph, "$(B)/bindings/legacy/wrap.py.")
                self.assertTrue(bytecode["outputs"][0].endswith(".yapyc"))
                self.assertIn(swig["uid"], bytecode["deps"])
                self.assertTrue(any("/py_modules/old.api.wrap" in str(n["cmds"]) for n in graph["graph"]))
                self.assertFalse(any("resfs/file/py/old.api.wrap" in str(n["cmds"]) for n in graph["graph"]))
                lib.node_by_output(graph, "$(B)/bindings/legacy/old.api.wrap_swg.reg.cpp")
                flatc = lib.only_node_by_kind(graph, "FP")
                self.assertTrue(flatc["outputs"][0].endswith(".py2.fbs.pysrc"))
                self.assertEqual(flatc["cmds"][0]["cmd_args"][:3], [
                    "$(B)/contrib/deprecated/flatc/flatc", "--python", "--gen-mutable",
                ])
                packed = lib.only_node_by_kind(graph, "P2")
                args = packed["cmds"][0]["cmd_args"]
                pycc = "$(B)/tools/py2cc/stage0pycc/py2cc" if light else "$(B)/tools/py2cc/py2cc"
                self.assertEqual(args[-3:], ["py2", "--pycc", pycc])
                self.assertIn(flatc["uid"], packed["deps"])
                lib.node_by_output(graph, packed["outputs"][0] + ".o")

    def test_native_srcs_and_src_lower_pyx_without_implicit_registration(self):
        files = native_files()
        files["build/platform/python/ymake_python3/ya.make"] = lib.resource_lib("YMAKE_PYTHON3", 1)
        files.update({
            "bindings/transport/ya.make": (
                f"PY23_NATIVE_LIBRARY(transport)\n{lib.BARE_MODULE}"
                "SRCDIR(interfaces)\nSRCS(wire.pyx plain.cpp)\n"
                "SRC(helper.pyx -EFEATURE=1)\nSRCS(GLOBAL static.pyx)\n"
                "CYTHON_FLAGS(-EEDITION=3)\nPY_REGISTER(wire)\nEND()\n"
            ),
            "interfaces/wire.pyx": 'cdef extern from "interfaces/api.h":\n    pass\n',
            "interfaces/api.h": '#include "detail.h"\n',
            "interfaces/detail.h": "",
            "bindings/transport/helper.pyx": "",
            "bindings/transport/static.pyx": "",
            "bindings/transport/plain.cpp": "",
        })
        graph = lib.make(files, "bindings/transport", "--target-platform", "default-linux-x86_64")
        archive = lib.node_by_output(graph, "$(B)/bindings/transport/libpy3ctransport.a")
        global_archive = lib.node_by_output(graph, "$(B)/bindings/transport/libpy3ctransport.global.a")
        for name in ("wire", "helper", "static"):
            output = f"$(B)/bindings/transport/{name}.pyx.py3.cpp"
            producer = lib.node_by_output(graph, output)
            command = producer["cmds"][0]["cmd_args"]
            source = "$(S)/interfaces/wire.pyx" if name == "wire" else f"$(S)/bindings/transport/{name}.pyx"
            expected = [
                "$(B)/resources/YMAKE_PYTHON3/bin/python3", "$(S)/contrib/tools/cython/cython.py",
                "-X", "legacy_implicit_noexcept=True", "-E", "UNAME_SYSNAME=Linux", "-EEDITION=3",
                "--cplus", "-I$(B)", "-I$(S)", "-I$(S)/contrib/tools/cython/Cython/Includes",
                source, "-o", output,
            ]
            if name == "helper":
                expected.append("-EFEATURE=1")
            self.assertEqual(command, expected)
            compiled = lib.node_by_output(graph, output + ".py3.o")
            self.assertIn(producer["uid"], compiled["deps"])
            owner = global_archive if name == "static" else archive
            self.assertIn(compiled["uid"], owner["deps"])
            self.assertNotIn("-EFEATURE=1", compiled["cmds"][0]["cmd_args"])
            if name == "wire":
                for header in ("api.h", "detail.h"):
                    self.assertIn("$(S)/interfaces/" + header, compiled["inputs"])
        registrations = [o for n in graph["graph"] for o in n["outputs"] if o.endswith(".reg3.cpp")]
        self.assertEqual(registrations, ["$(B)/bindings/transport/wire.reg3.cpp"])

    def test_swig_defaults_to_cpp_and_resolves_srcdir_and_namespace(self):
        files = swig_files()
        files.update({
            "bindings/codec/ya.make": (
                f"PY23_LIBRARY()\n{lib.BARE_MODULE}SRCDIR(interfaces)\n"
                "PY_SRCS(NAMESPACE protocol sub/codec.swg=contract.rebound SWIG_C plain.swg)\nEND()\n"
            ),
            "interfaces/sub/codec.swg": '%include "shared.i"\n',
            "interfaces/sub/shared.i": '%header %{\n#include "interfaces/public.h"\n%}\n',
            "interfaces/plain.swg": "",
            "interfaces/public.h": "",
        })
        graph = lib.make(files, "bindings/codec")
        for output, source, cpp, module in (
            ("sub/rebound.swg.cpp", "interfaces/sub/codec.swg", True, "rebound"),
            ("plain.swg.c", "interfaces/plain.swg", False, "plain"),
        ):
            stem = "$(B)/bindings/codec/" + output
            producer = lib.node_by_output(graph, stem)
            expected = [
                "$(B)/contrib/tools/swig/swig", "-I$(B)", "-I$(S)",
                "-I$(S)/contrib/tools/swig/Lib/python", "-I$(S)/contrib/tools/swig/Lib",
                *(["-c++"] if cpp else []), "-python", "-module", module,
                "-interface", module + "_swg", "-o", stem, "$(S)/" + source,
            ]
            self.assertEqual(producer["cmds"][0]["cmd_args"], expected)
            object_path = "_/" + output if cpp else output
            compiled = lib.node_by_output(graph, "$(B)/bindings/codec/" + object_path + ".py3.o")
            self.assertIn(producer["uid"], compiled["deps"])
            if cpp:
                self.assertIn("$(S)/interfaces/sub/shared.i", producer["inputs"])
                self.assertIn("$(S)/interfaces/public.h", compiled["inputs"])
        bytecode = lib.node_by_output_prefix(graph, "$(B)/bindings/codec/sub/rebound.py.")
        self.assertIn("$(B)/bindings/codec/sub/rebound.swg.cpp", bytecode["inputs"])
        self.assertTrue(any("resfs/file/py/contract/rebound.py" in str(n["cmds"]) for n in graph["graph"]))
        lib.node_by_output(graph, "$(B)/bindings/codec/contract.rebound_swg.reg3.cpp")
        lib.node_by_output(graph, "$(B)/bindings/codec/protocol.plain_swg.reg3.cpp")

    def test_library_fbs_and_stdout_noauto_python_are_both_result_dependencies(self):
        files = native_files()
        files.update(flatc_files())
        lib.tool_program(files, "tools/proxy_writer", "proxy_writer")
        files.update({
            "bindings/envelope/ya.make": (
                f"PY3_LIBRARY(envelope)\n{lib.BARE_MODULE}PY_NAMESPACE(envelope_api)\n"
                "SET(MODULE_COMMON_CONFIGS_DIR policies)\nSTYLE_PYTHON(CONFIG_TYPE pyproject.toml)\n"
                "SRCDIR(schemas)\nPY_SRCS(Proxy.py packet.fbs)\n"
                "RUN_PROGRAM(tools/proxy_writer --class=Packet STDOUT_NOAUTO Proxy.py)\nEND()\n"
            ),
            "schemas/packet.fbs": 'include "common.fbs";\ntable Packet {}\n',
            "schemas/common.fbs": "table Common {}\n",
            "contrib/python/flatbuffers/ya.make": f"PY3_LIBRARY()\n{lib.BARE_MODULE}END()\n",
            "build/scripts/tar_sources.py": "",
            "build/scripts/compile_pysrc.py": "",
        })
        graph = lib.make(files, "bindings/envelope", "-t", "--add-modules-to-results")
        digest = base64.b32encode(hashlib.md5(str([("packet.fbs", "envelope_api.packet")]).encode()).digest()).decode().lower().rstrip("=")
        stem = "$(B)/bindings/envelope/" + digest + ".py3.fbs"
        flatc = lib.node_by_output(graph, stem + ".pysrc")
        self.assertEqual(flatc["cmds"][0]["cmd_args"][-1], "$(S)/schemas/packet.fbs")
        self.assertIn("$(S)/schemas/common.fbs", flatc["inputs"])
        packed = lib.node_by_output(graph, stem + ".py3_raw.cpp")
        self.assertIn(flatc["uid"], packed["deps"])
        compiled = lib.node_by_output(graph, stem + ".py3_raw.cpp.o")
        self.assertIn(packed["uid"], compiled["deps"])
        proxy = lib.node_by_output(graph, "$(B)/bindings/envelope/Proxy.py")
        bytecode = lib.node_by_output(graph, "$(B)/bindings/envelope/Proxy.py.yapyc3")
        self.assertEqual(bytecode["cmds"][0]["cmd_args"][-3:], ["Proxy.py-", "$(B)/bindings/envelope/Proxy.py", "$(B)/bindings/envelope/Proxy.py.yapyc3"])
        self.assertIn(proxy["uid"], bytecode["deps"])
        archive = lib.node_by_output(graph, "$(B)/bindings/envelope/libpy3envelope.global.a")
        self.assertIn(compiled["uid"], archive["deps"])
        self.assertIn(archive["uid"], graph["result"])
        resources = [n for n in graph["graph"] if "$(B)/bindings/envelope/Proxy.py" in n["inputs"] and any("objcopy_" in o for o in n["outputs"])]
        self.assertEqual(len(resources), 1)
        self.assertIn(bytecode["uid"], resources[0]["deps"])
        self.assertIn(resources[0]["uid"], archive["deps"])
        self.assertFalse(any("$(S)/bindings/envelope/Proxy.py" in n["inputs"] for n in graph["graph"]))
        self.assertFalse(any("bindings/envelope/test-results/" in o for n in graph["graph"] for o in n["outputs"]))

        # A source Python file makes the same style config mandatory.
        files["bindings/envelope/ya.make"] = files["bindings/envelope/ya.make"].replace("PY_SRCS(Proxy.py packet.fbs)", "PY_SRCS(Proxy.py packet.fbs manual.py)")
        files["bindings/envelope/manual.py"] = ""
        failed = lib.make_process(files, "bindings/envelope", "-t")
        self.assertNotEqual(failed.returncode, 0)
        self.assertIn("STYLE: missing config policies/pyproject.toml", failed.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
