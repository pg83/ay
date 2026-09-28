import json
import os
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


ANSI = re.compile(r"\x1b\[[0-9;]*m")
NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
PROTO_LIBRARIES = [
    "apphost/tools/stub_generator/cpp_includes",
    "contrib/libs/googleapis-common-protos",
    "contrib/libs/grpc",
    "contrib/libs/protobuf",
    "dep/p0",
    "dep/p1",
    "dep/p2a",
    "dep/p2b",
    "library/cpp/eventlog",
    "yandex_io/libs/protobuf_utils",
]
PROTO_TOOLS = [
    "apphost/tools/stub_generator/cpp_plugin",
    "contrib/tools/protoc",
    "contrib/tools/protoc/plugins/cpp_styleguide",
    "contrib/tools/protoc/plugins/grpc_cpp",
    "library/cpp/yaff/tools/protoc_plugin",
    "tools/event2cpp",
    "tools/p0",
    "tools/p1",
    "tools/p2",
    "yandex_io/tools/capability_gen",
]


def proto_tree(body):
    files = {
        "a/ya.make": "PROTO_LIBRARY()\n" + NO_PLATFORM + body + "\nSRCS(x.proto)\nEND()\n",
        "a/x.proto": 'syntax = "proto3";\n',
    }
    for path in PROTO_LIBRARIES:
        files[f"{path}/ya.make"] = "LIBRARY()\n" + NO_PLATFORM + "END()\n"
    for path in PROTO_TOOLS:
        lib.tool_program(files, path, path.rsplit("/", 1)[-1])
    return files


def ay_make(files, *args):
    with tempfile.TemporaryDirectory(prefix="ay-yamake-test-") as directory:
        root = Path(directory)
        (root / ".arcadia.root").touch()
        (root / "ya.conf").write_text(
            '[flags]\nOPENSOURCE = "yes"\n\n'
            '[host_platform_flags]\nOPENSOURCE = "yes"\n'
        )
        for relative, content in files.items():
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        env = {
            key: value
            for key, value in os.environ.items()
            if key not in lib.TOOLCHAIN_ENV_VARS
        }
        result = subprocess.run(
            [
                str(lib.AY), "make", "-j0", "-G", "--sandboxing",
                "--source-root", str(root),
                "--target-platform", "default-linux-aarch64",
                "--host-platform", "default-linux-x86_64",
                *args, "a",
            ],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=30,
            check=False,
        )
        graph = json.loads(result.stdout) if result.returncode == 0 else None
        return result.returncode, graph, ANSI.sub("", result.stderr).strip()


class YaMakeProtoTest(unittest.TestCase):
    def make(self, body):
        code, graph, stderr = ay_make(proto_tree(body), "-k")
        self.assertEqual(code, 0, stderr)
        return graph

    def test_cpp_proto_plugins(self):
        graph = self.make(
            "CPP_PROTO_PLUGIN0(p0 tools/p0 DEPS dep/p0)\n"
            "CPP_PROTO_PLUGIN(p1 tools/p1 .p1.h DEPS dep/p1 EXTRA_OUT_FLAG flag1)\n"
            "CPP_PROTO_PLUGIN2(p2 tools/p2 .p2.h .p2.cpp EXTRA_OUT_FLAG flag2 DEPS dep/p2a dep/p2b)\n"
            "YAFF(NAMESPACE yns FILES x other EXPERIMENTAL x)\n"
            "YAFF_SCHEMA(sch sns FILES x)\n"
            "YAFF_SCHEMA(sch2 NAMESPACE ons EXPERIMENTAL x)\n"
            "APPHOST(ITEM_DISPATCHER disp ITEM_DISPATCHER_HEADER disp.h)\n"
            "ALICE_CAPABILITY()\n"
            "CPP_EVLOG()\n"
            "GRPC()\n"
            "PROTOC_FATAL_WARNINGS()\n"
            "SET_APPEND(_PROTOC_FLAGS --extra)"
        )
        protoc = lib.only_node_by_kind(graph, "PB")
        self.assertEqual(protoc["outputs"], [
            "$(B)/a/x.pb.h", "$(B)/a/x.pb.cc", "$(B)/a/x.grpc.pb.cc", "$(B)/a/x.grpc.pb.h",
            "$(B)/a/x.p1.h", "$(B)/a/x.p2.h", "$(B)/a/x.p2.cpp",
            "$(B)/a/x.yaff.h", "$(B)/a/x.yaff.cpp",
            "$(B)/a/x_sch.yaff.h", "$(B)/a/x_sch.yaff.cpp",
            "$(B)/a/x_sch2.yaff.h", "$(B)/a/x_sch2.yaff.cpp",
            "$(B)/a/x.apphost.h", "$(B)/a/x.cap.h",
        ])
        args = protoc["cmds"][0]["cmd_args"]
        for expected in (
            "--fatal_warnings", "--extra",
            "--plugin=protoc-gen-p0=$(B)/tools/p0/p0", "--p0_out=$(B)/",
            "--p1_opt=flag1", "--p2_opt=flag2",
            "--yaff_opt=namespace=yns", "--yaff_opt=file=x", "--yaff_opt=file=other",
            "--yaff_opt=experimental=x",
            "--yaff_sch_opt=tag=sch", "--yaff_sch_opt=namespace=sns",
            "--yaff_sch2_opt=tag=sch2", "--yaff_sch2_opt=namespace=ons",
            "--grpc_cpp_out=$(B)/", "--yaff_sch2_opt=experimental=x",
            "--cpp_plugin_opt=item_dispatcher=disp",
            "--cpp_plugin_opt=item_dispatcher_header=disp.h",
            "--alice_capability_cpp_out=$(B)/", "--event2cpp_out=$(B)/",
        ):
            self.assertIn(expected, args)

    def test_proto_include_namespaces(self):
        cases = [
            ("PROTO_NAMESPACE(GLOBAL my/ns)", "$(S)/my/ns", "-I$(B)/my/ns"),
            ("EXPORT_YMAPS_PROTO()", "$(S)/maps/doc/proto", "-I$(B)/maps/doc/proto"),
        ]
        for body, proto_root, cc_include in cases:
            with self.subTest(body=body):
                graph = self.make(body)
                protoc = lib.only_node_by_kind(graph, "PB")["cmds"][0]["cmd_args"]
                self.assertIn("-I=" + proto_root, protoc)
                cc = lib.node_by_output(graph, "$(B)/a/x.pb.cc.o")["cmds"][0]["cmd_args"]
                self.assertIn(cc_include, cc)


if __name__ == "__main__":
    unittest.main(verbosity=2)
