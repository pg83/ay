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
    "sdg/sdc/ros/configuration/golaunch/topology/common_deps",
    "yandex_io/libs/protobuf_utils",
]
PROTO_TOOLS = [
    "apphost/tools/stub_generator/cpp_plugin",
    "contrib/tools/protoc",
    "contrib/tools/protoc/plugins/cpp_styleguide",
    "contrib/tools/protoc/plugins/grpc_cpp",
    "library/cpp/yaff/tools/protoc_plugin",
    "sdg/sdc/ros/configuration/golaunch/topology/generator",
    "tools/event2cpp",
    "tools/p0",
    "tools/p1",
    "tools/p2",
    "yandex_io/tools/capability_gen",
]


def proto_tree(body):
    files = {
        "a/ya.make": "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\n" + NO_PLATFORM + body + "\nSRCS(x.proto)\nEND()\n",
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
        lib.seed_root(root)
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
            "$(B)/a/x.pb.h", "$(B)/a/x.pb.cc",
            "$(B)/a/x.p1.h", "$(B)/a/x.p2.h", "$(B)/a/x.p2.cpp",
            "$(B)/a/x.yaff.h", "$(B)/a/x.yaff.cpp",
            "$(B)/a/x_sch.yaff.h", "$(B)/a/x_sch.yaff.cpp",
            "$(B)/a/x_sch2.yaff.h", "$(B)/a/x_sch2.yaff.cpp",
            "$(B)/a/x.apphost.h", "$(B)/a/x.cap.h",
            "$(B)/a/x.grpc.pb.cc", "$(B)/a/x.grpc.pb.h",
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

    def test_generate_topology_is_a_cpp_proto_plugin(self):
        graph = self.make("GENERATE_TOPOLOGY(ROS_MESSAGE_HEADERS m/a m/b)\n")
        protoc = lib.only_node_by_kind(graph, "PB")
        self.assertIn("$(B)/a/x.topology.pb.h", protoc["outputs"])
        args = protoc["cmds"][0]["cmd_args"]
        for expected in (
            "--plugin=protoc-gen-golaunch_generator=$(B)/sdg/sdc/ros/configuration/golaunch/topology/generator/generator",
            "--golaunch_generator_out=$(B)/", "--golaunch_generator_opt=m/a", "--golaunch_generator_opt=m/b",
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

    def test_yaff_runtime_headers_are_the_plugin_induced_deps(self):
        # YAFF is CPP_PROTO_PLUGIN2 over library/cpp/yaff/tools/protoc_plugin
        # (build/internal/conf/project_specific/yabs.conf); the runtime
        # headers of its outputs are that tool's INDUCED_DEPS(h+cpp ...).
        files = proto_tree("YAFF(NAMESPACE yns)\nSRCS(u.cpp)")
        files["a/u.cpp"] = '#include "a/x.yaff.h"\n'
        files["library/cpp/yaff/tools/protoc_plugin/ya.make"] = (
            "PROGRAM(protoc_plugin)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\n"
            "INDUCED_DEPS(h+cpp ${ARCADIA_ROOT}/library/cpp/yaff/yaff.h)\nEND()\n"
        )
        files["library/cpp/yaff/yaff.h"] = ""
        files["library/cpp/yaff/struct.h"] = ""
        code, graph, stderr = ay_make(files, "-k")
        self.assertEqual(code, 0, stderr)
        inputs = lib.node_by_output(graph, "$(B)/a/u.cpp.o")["inputs"]
        self.assertIn("$(S)/library/cpp/yaff/yaff.h", inputs)
        self.assertNotIn("$(S)/library/cpp/yaff/struct.h", inputs)

    def test_yaff_file_filter_does_not_change_generated_header_includes(self):
        # The yabs_server_daemons_bs_static reference: profile_keyword_ids.proto
        # is not in YAFF(FILES ...) of ads/profile/proto, its .yaff.cpp.o
        # still sees its .pb.h; FILES only reaches the plugin options.
        expected = ["$(S)/a/u.cpp", "$(B)/a/x.yaff.h", "$(B)/a/x.pb.h"]
        cases = [
            ("YAFF(NAMESPACE yns FILES x.proto EXPERIMENTAL x.proto)", expected),
            ("YAFF(NAMESPACE yns FILES x.proto)", expected),
            ("YAFF(NAMESPACE yns FILES other.proto)", expected),
        ]
        for body, expected in cases:
            with self.subTest(body=body):
                files = proto_tree(body + "\nSRCS(u.cpp)")
                files["a/u.cpp"] = '#include "a/x.yaff.h"\n'
                code, graph, stderr = ay_make(files, "-k")
                self.assertEqual(code, 0, stderr)
                self.assertEqual(lib.node_by_output(graph, "$(B)/a/u.cpp.o")["inputs"], expected)


if __name__ == "__main__":
    unittest.main(verbosity=2)
