import unittest

import lib


SPROTOC = "$(B)/maps/libs/sproto/sprotoc/sprotoc"


def sproto_files():
    files = {
        "build/scripts/cpp_proto_wrapper.py": "print('proto')\n",
        "contrib/libs/protobuf/ya.make": (
            "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
            "ADDINCL(GLOBAL contrib/libs/protobuf/src)\n"
            "SRCS(protobuf.cpp)\nEND()\n"
        ),
        "contrib/libs/protobuf/protobuf.cpp": "int protobuf(){return 0;}\n",
        "maps/libs/sproto/ya.make": (
            "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(x.cpp)\nEND()\n"
        ),
        "maps/libs/sproto/x.cpp": "int x(){return 0;}\n",
    }
    for header in (
        "generated_message_bases.h", "map_entry.h", "map_entry_lite.h",
        "map_field.h", "map_field_inl.h", "map_field_lite.h",
        "reflection_ops.h",
    ):
        files[f"contrib/libs/protobuf/src/google/protobuf/{header}"] = "#pragma once\n"
    lib.tool_program(files, "contrib/tools/protoc", "protoc")
    lib.tool_program(
        files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide"
    )
    lib.tool_program(files, "maps/libs/sproto/sprotoc", "sprotoc")
    return files


class ProtoSprotoTest(unittest.TestCase):
    def test_sproto_headers_follow_pb_imports(self):
        files = sproto_files()
        files.update({
            "m/ya.make": (
                "PROTO_LIBRARY()\nEXPORT_YMAPS_PROTO()\n"
                "SRCS(maps/doc/proto/m/a.proto maps/doc/proto/m/b.proto)\n"
                "YMAPS_SPROTO(maps/doc/proto/m/a.proto maps/doc/proto/m/b.proto)\n"
                "END()\n"
            ),
            "maps/doc/proto/m/a.proto": (
                'syntax = "proto2";\nimport "m/b.proto";\nmessage A {}\n'
            ),
            "maps/doc/proto/m/b.proto": 'syntax = "proto2";\nmessage B {}\n',
            "u/ya.make": (
                "PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "PEERDIR(m)\nSRCS(u.cpp)\nEND()\n"
            ),
            "u/u.cpp": "#include <m/a.sproto.h>\nint main(){return 0;}\n",
        })
        graph = lib.make(files, "u")

        sproto = lib.node_by_output(graph, "$(B)/maps/doc/proto/m/a.sproto.h")
        self.assertEqual(sproto["kv"], {"p": "PB", "pc": "yellow"})
        self.assertEqual(sproto["outputs"], ["$(B)/maps/doc/proto/m/a.sproto.h"])
        self.assertEqual(sproto["cmds"][0]["cwd"], "$(S)")
        self.assertEqual(sproto["cmds"][0]["cmd_args"], [
            SPROTOC, "-I=./maps/doc/proto", "-I=$(S)/maps/doc/proto", "-I=$(B)",
            "-I=$(S)/contrib/libs/protobuf/src",
            "--sproto_out=$(B)/maps/doc/proto", "maps/doc/proto/m/a.proto",
        ])
        self.assertEqual(sproto["inputs"][0], SPROTOC)
        self.assertIn("$(S)/maps/doc/proto/m/a.proto", sproto["inputs"])
        self.assertIn("$(S)/maps/doc/proto/m/b.proto", sproto["inputs"])
        self.assertNotIn("$(B)/maps/doc/proto/m/b.pb.h", sproto["inputs"])
        self.assertIn(lib.node_by_output(graph, SPROTOC)["uid"], sproto["deps"])

        pb = lib.node_by_output(graph, "$(B)/maps/doc/proto/m/a.pb.h")
        self.assertIn("--cpp_out=:$(B)/maps/doc/proto", pb["cmds"][0]["cmd_args"])

        pb_object = lib.node_by_output(graph, "$(B)/m/__/maps/doc/proto/m/a.pb.cc.o")
        self.assertIn("$(B)/maps/doc/proto/m/b.sproto.h", pb_object["inputs"])
        self.assertIn("$(B)/maps/doc/proto/m/b.pb.h", pb_object["inputs"])

        consumer = lib.node_by_output(graph, "$(B)/u/u.cpp.o")
        for path in (
            "$(B)/maps/doc/proto/m/a.sproto.h",
            "$(B)/maps/doc/proto/m/b.sproto.h",
            "$(B)/maps/doc/proto/m/b.pb.h",
        ):
            self.assertIn(path, consumer["inputs"])
        link = lib.node_by_output(graph, "$(B)/u/u")
        self.assertIn("$(B)/maps/libs/sproto/libmaps-libs-sproto.a", link["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
