import base64
import hashlib
import unittest

import lib
from test_proto_flatc import flatc_files
from test_python_srcs import python_base


def fixture():
    files = python_base()
    files.update(flatc_files())
    files["tools/rescompiler/ya.make"] = files["tools/rescompiler/ya.make"].replace("END()", "INDUCED_DEPS(cpp ${ARCADIA_ROOT}/resource.h)\nEND()")
    files["resource.h"] = "#pragma once\n"
    files.update({
        "entry/ya.make": "PY3_PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nPEERDIR(fbs)\nEND()\n",
        "fbs/ya.make": "FBS_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nIF (PY3_FBS)\nFLATC_FLAGS(--python-control)\nGENERATE_ENUM_SERIALIZATION(no-python-enum.h)\nSRCS(a.fbs)\nELSE()\nSRCS(b.fbs)\nENDIF()\nEND()\n",
        "fbs/a.fbs": 'include "common.fbs";\ntable A {}\n',
        "fbs/b.fbs": "table B {}\n",
        "fbs/common.fbs": "table Common {}\n",
        "contrib/python/flatbuffers/ya.make": "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n",
        "build/scripts/tar_sources.py": "",
        "build/scripts/compile_pysrc.py": "",
    })
    return files


class PythonFbsTest(unittest.TestCase):
    def test_python_variant_reexecutes_sources_and_compiles_archive(self):
        graph = lib.make(fixture(), "entry", "-t")
        self.assertFalse(any("fbs/test-results/flake8/" in o for n in graph["graph"] for o in n["outputs"]))
        name = base64.b32encode(hashlib.md5(str([("a.fbs", "fbs.a")]).encode()).digest()).decode().lower().rstrip("=")
        stem = "$(B)/fbs/" + name + ".py3.fbs"
        fp = lib.node_by_output(graph, stem + ".pysrc")
        self.assertEqual(fp["kv"], {"p": "FP", "pc": "light-green", "tared_kind": "nodir"})
        args = fp["cmds"][0]["cmd_args"]
        self.assertEqual(args[:6], ["$(B)/contrib/libs/flatbuffers/flatc/flatc", "--python", "--no-warnings", "--python-typing", "--gen-mutable", "--python-control"])
        self.assertEqual(args[-1], "$(S)/fbs/a.fbs")
        self.assertIn("$(S)/fbs/common.fbs", fp["inputs"])
        p3 = lib.node_by_output(graph, stem + ".py3_raw.cpp")
        self.assertEqual(p3["kv"], {"p": "P3", "pc": "light-green"})
        self.assertIn(fp["uid"], p3["deps"])
        cc = lib.node_by_output(graph, stem + ".py3_raw.cpp.py3.o")
        self.assertIn(p3["uid"], cc["deps"])
        self.assertIn("$(S)/resource.h", cc["inputs"])
        ar = lib.node_by_output(graph, "$(B)/fbs/libpy3fbs.global.a")
        self.assertIn(cc["uid"], ar["deps"])
        self.assertFalse(any("$(S)/fbs/b.fbs" in n["inputs"] for n in graph["graph"]))


    def test_native_defaults_precede_python_runtime_peer_includes(self):
        files = fixture()
        files["fbs/ya.make"] = files["fbs/ya.make"].replace("NO_RUNTIME()\n", "").replace("NO_LIBC()\n", "")
        bare = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
        for runtime in ("contrib/libs/cxxsupp/libcxx", "contrib/libs/cxxsupp/libcxxrt", "contrib/libs/libunwind", "library/cpp/sanitizer/include"):
            files[runtime + "/ya.make"] = "LIBRARY()\n" + bare + "END()\n"
        files["contrib/libs/cxxsupp/libcxx/ya.make"] = "LIBRARY()\n" + bare + "ADDINCL(GLOBAL ${ARCADIA_ROOT}/native-include)\nEND()\n"
        files["contrib/libs/python/ya.make"] = "LIBRARY()\nNO_UTIL()\nADDINCL(GLOBAL ${ARCADIA_ROOT}/python-include)\nEND()\n"
        files["native-include/header.h"] = ""
        files["python-include/header.h"] = ""
        graph = lib.make(files, "entry")
        cc = next(n for n in graph["graph"] if any(".py3_raw.cpp.py3.o" in o for o in n["outputs"]))
        args = cc["cmds"][0]["cmd_args"]
        self.assertLess(args.index("-I$(S)/native-include"), args.index("-I$(S)/python-include"))

    def test_groups_keep_python_namespaces_and_reuse_identical_archives(self):
        files = fixture()
        files["fbs/ya.make"] = "FBS_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCDIR(other)\nIF (PY3_FBS)\nSRCS(a.fbs)\nPY_SRCS(a.fbs)\nPY_SRCS(NAMESPACE \"q'\" z.fbs a.fbs a.fbs=explicit.module)\nENDIF()\nEND()\n"
        files["other/z.fbs"] = "table Z {}\n"
        graph = lib.make(files, "entry")
        fp = [n for n in graph["graph"] if n["kv"].get("p") == "FP"]
        self.assertEqual(len(fp), 2)
        self.assertTrue(any("$(S)/other/z.fbs" in n["inputs"] for n in fp))
        expected = [
            [("a.fbs", "fbs.a")],
            [("z.fbs", "q'.z"), ("a.fbs", "q'.a"), ("a.fbs", "explicit.module")],
        ]
        names = {base64.b32encode(hashlib.md5(str(sorted(items)).encode()).digest()).decode().lower().rstrip("=") for items in expected}
        self.assertEqual({n["outputs"][0].rsplit("/", 1)[-1].split(".")[0] for n in fp}, names)

    def test_cpp_peer_and_excluded_python_variant_keep_their_own_sources(self):
        files = fixture()
        files["entry/ya.make"] = "PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nPEERDIR(fbs)\nEND()\n"
        graph = lib.make(files, "entry")
        self.assertTrue(any("$(S)/fbs/b.fbs" in n["inputs"] for n in graph["graph"]))
        self.assertFalse(any(n["kv"].get("p") == "FP" for n in graph["graph"]))
        files = fixture()
        files["fbs/ya.make"] = files["fbs/ya.make"].replace("END()", "ONLY_TAGS(CPP_FBS)\nEND()")
        graph = lib.make(files, "entry")
        self.assertFalse(any(n["kv"].get("p") == "FP" for n in graph["graph"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
