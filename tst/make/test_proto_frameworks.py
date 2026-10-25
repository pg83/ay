import unittest

from tst import lib
from tst.make.test_go_package import STD, go_module, go_world, go_tool_args


YSON_TOOL = "yt/yt/orm/go/codegen/yson/internal/proto-yson-gen/cmd/proto-yson-gen"
YSON_PEERS = [
    f"{STD}/strings", f"{STD}/fmt", f"{STD}/errors", f"{STD}/encoding/json",
    "library/go/core/xerrors", "yt/go/yson", "yt/go/yterrors",
    "yt/yt/orm/go/codegen/yson/ytypes",
]


class ProtoFrameworksTest(unittest.TestCase):
    def test_yson_batches_protos_and_compiles_generated_outputs(self):
        files = go_world()
        for peer in YSON_PEERS:
            go_module(files, peer)
        lib.tool_program(files, "contrib/tools/protoc", "protoc")
        lib.tool_program(files, YSON_TOOL, "proto-yson-gen")
        files.update({
            "contrib/libs/protobuf/ya.make": "LIBRARY()\nNO_UTIL()\nNO_RUNTIME()\nNO_LIBC()\nSRCS(runtime.cpp)\nEND()\n",
            "contrib/libs/protobuf/runtime.cpp": "int runtime;\n",
            "model/one.proto": 'syntax = "proto3";\nimport "shared.proto";\n',
            "model/two.proto": 'syntax = "proto3";\n',
            "model/shared.proto": 'syntax = "proto3";\n',
            "lib/ya.make": (
                "GO_LIBRARY()\nPROTO_NAMESPACE(model)\nPROTOC_FATAL_WARNINGS()\n"
                "YT_ORM_PROTO_YSON(model/one.proto model/two.proto OUT_OPTS a=b c=d)\nEND()\n"
            ),
        })
        graph = lib.make(files, "lib")
        producer = lib.node_by_output(graph, "$(B)/lib/one.yson.go")
        self.assertEqual(producer["outputs"], [
            "$(B)/lib/one.yson.go", "$(B)/lib/two.yson.go",
        ])
        args = producer["cmds"][0]["cmd_args"]
        self.assertEqual(producer["cmds"][0]["cwd"], "$(B)/lib")
        self.assertEqual(args[:2], [
            "$(B)/contrib/tools/protoc/protoc",
            f"--plugin=protoc-gen-custom=$(B)/{YSON_TOOL}/proto-yson-gen",
        ])
        self.assertEqual(args[-5:], [
            "--custom_out=a=b c=d paths=base_name:.",
            "--custom_opt=goroot=$(B)/resources/GO_TOOLS", "--fatal_warnings",
            "$(S)/model/one.proto", "$(S)/model/two.proto",
        ])
        self.assertIn("$(S)/model/shared.proto", producer["inputs"])
        package = lib.node_by_output(graph, "$(B)/lib/lib.a")
        self.assertIn(producer["uid"], package["deps"])
        runtime = lib.node_by_output(graph, "$(B)/contrib/libs/protobuf/libcontrib-libs-protobuf.a")
        self.assertIn(runtime["uid"], package["deps"])
        self.assertIn(runtime["outputs"][0], package["inputs"])
        self.assertNotIn(runtime["outputs"][0], go_tool_args(package))
        for output in producer["outputs"]:
            self.assertIn(output, go_tool_args(package))
        for peer in YSON_PEERS:
            self.assertIn(f"$(B)/{peer}/{peer.rsplit('/', 1)[-1]}.a", package["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
