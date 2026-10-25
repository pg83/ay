import unittest
from itertools import product

from tst import lib
from tst.make.test_proto_py2_context import proto_world


def workspace(with_gen=False):
    files = proto_world()
    generated = "WITH_GEN " if with_gen else ""
    files["py3/ya.make"] = f"PY3_PROGRAM()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\nPEERDIR(p)\nEND()\n"
    files["p/ya.make"] = (
        f"PROTO_LIBRARY()\n{lib.BARE_MODULE}NO_MYPY()\nNO_OPTIMIZE_PY_PROTOS()\n"
        f"PEERDIR(common)\nPROTO_ADDINCL({generated}includes/local)\nSRCS(message.proto)\nEND()\n"
    )
    files["p/message.proto"] = 'syntax = "proto2";\nimport "shared.proto";\nimport "local.proto";\n'
    files["includes/public/shared.proto"] = 'syntax = "proto2";\n'
    files["includes/local/local.proto"] = 'syntax = "proto2";\n'
    files["includes/hidden/hidden.proto"] = 'syntax = "proto2";\n'
    files["common/ya.make"] = (
        f"PY23_LIBRARY()\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\n"
        f"PROTO_ADDINCL(GLOBAL {generated}includes/public)\nPROTO_ADDINCL({generated}includes/hidden)\nEND()\n"
    )
    files["contrib/python/protobuf/ya.make"] = (
        f"PY23_LIBRARY(protobuf)\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\n"
        "ADDINCL(GLOBAL includes/native)\n"
        "IF(PYTHON3)\nPEERDIR(published)\nENDIF()\nRESOURCE(DONT_COMPRESS - runtime=protobuf)\nSRCS(native.cpp)\nEND()\n"
    )
    files["contrib/python/protobuf/native.cpp"] = '#include "native.h"\n'
    files["includes/native/native.h"] = "int native;\n"
    files["published/ya.make"] = (
        f"PROTO_LIBRARY()\n{lib.BARE_MODULE}EXCLUDE_TAGS(CPP_PROTO GO_PROTO)\n"
        "NO_MYPY()\nNO_OPTIMIZE_PY_PROTOS()\nDISABLE(NEED_GOOGLE_PROTO_PEERDIRS)\n"
        "PROTO_NAMESPACE(GLOBAL includes/versioned)\nSRCDIR(includes/versioned)\nSRCS(version.proto)\nEND()\n"
    )
    files["includes/versioned/version.proto"] = 'syntax = "proto2";\n'
    return files


class PythonProtoIncludesTest(unittest.TestCase):
    def test_protoc_uses_local_and_exported_proto_scopes_from_its_runtime(self):
        for (version, target), with_gen in product((("py2", "py"), ("py3", "py3")), (False, True)):
            with self.subTest(version=version, with_gen=with_gen):
                graph = lib.make(workspace(with_gen), target)
                node = lib.node_by_output(graph, f"$(B)/p/message__int{version}___pb2.py")
                args = node["cmds"][0]["cmd_args"]
                includes = [arg for arg in args if arg.startswith("-I=")]
                expected = ["-I=./", "-I=$(S)/", "-I=$(B)", "-I=$(S)",
                            "-I=$(S)/includes/local", "-I=$(S)/includes/public",
                            "-I=$(B)", "-I=$(S)/contrib/libs/protobuf/src"]
                if version == "py3":
                    expected.append("-I=$(S)/includes/versioned")
                if with_gen:
                    expected.extend(["-I=$(B)/includes/local", "-I=$(B)/includes/public"])
                self.assertCountEqual(includes, expected)
                for source in ("p/message.proto", "includes/public/shared.proto", "includes/local/local.proto"):
                    self.assertIn("$(S)/" + source, node["inputs"])
                self.assertNotIn("$(S)/includes/hidden/hidden.proto", node["inputs"])
                objects = [n for n in graph["graph"] if any(o.startswith("$(B)/p/") and o.endswith(f".{version}.o") for o in n["outputs"])]
                self.assertTrue(objects)
                native = next(n for n in graph["graph"] if n["kv"]["p"] == "CC" and "$(S)/contrib/python/protobuf/native.cpp" in n["inputs"])
                self.assertIn("$(S)/includes/native/native.h", native["inputs"])
                self.assertTrue(any("-I$(S)/includes/native" in cmd["cmd_args"] for cmd in native["cmds"]))


if __name__ == "__main__":
    unittest.main()
