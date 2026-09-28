import unittest

import lib


EMPTY_LIBRARY = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(x.cpp)\nEND()\n"


def family_files():
    files = {
        "build/scripts/cpp_proto_wrapper.py": "print('proto')\n",
        "contrib/libs/protobuf/ya.make": (
            "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
            "ADDINCL(GLOBAL contrib/libs/protobuf/src)\n"
            "SRCS(protobuf.cpp)\nEND()\n"
        ),
        "contrib/libs/protobuf/protobuf.cpp": "int protobuf(){return 0;}\n",
        "contrib/libs/protobuf/src/google/protobuf/descriptor.proto": (
            'syntax = "proto2";\n'
        ),
        "contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/cleanup.h": (
            "#pragma once\n"
        ),
        "contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/internal/cleanup.h": (
            "#pragma once\n"
        ),
        "library/cpp/proto_config/protos/ya.make": (
            "PROTO_LIBRARY()\nSRCS(extensions.proto)\nEND()\n"
        ),
        "library/cpp/proto_config/protos/extensions.proto": 'syntax = "proto2";\n',
    }
    for header in (
        "arena.h", "arenastring.h", "extension_set.h",
        "generated_message_reflection.h", "generated_message_util.h",
        "io/coded_stream.h", "message.h", "metadata_lite.h", "port_def.inc",
        "port_undef.inc", "repeated_field.h", "unknown_field_set.h",
        "generated_message_bases.h", "map_entry.h", "map_entry_lite.h",
        "map_field.h", "map_field_inl.h", "map_field_lite.h",
        "reflection_ops.h", "io/printer.h", "io/zero_copy_sink.h",
        "stubs/hash.h", "stubs/stringpiece.h", "stubs/strutil.h",
        "wire_format.h",
    ):
        files[f"contrib/libs/protobuf/src/google/protobuf/{header}"] = "#pragma once\n"
    for path in ("library/cpp/eventlog", "library/cpp/proto_config/codegen"):
        files[f"{path}/ya.make"] = EMPTY_LIBRARY
        files[f"{path}/x.cpp"] = "int x(){return 0;}\n"
    for path in (
        "contrib/tools/protoc", "contrib/tools/protoc/plugins/cpp_styleguide",
        "library/cpp/proto_config/plugin", "tools/event2cpp",
    ):
        lib.tool_program(files, path, path.rsplit("/", 1)[-1])
    return files


class ProtoImportsTest(unittest.TestCase):
    def test_event_config_and_opaque_imports_under_namespace(self):
        files = family_files()
        files.update({
            "e/ya.make": (
                "PROTO_LIBRARY()\nPROTO_NAMESPACE(e)\n"
                "SRCS(a.ev b.ev c.cfgproto d.proto)\nEND()\n"
            ),
            "e/a.ev": (
                'import "./b.ev";\nimport "c.cfgproto";\nimport "e/opts.inc";\n'
                "message TA {}\n"
            ),
            "e/b.ev": "message TB {}\n",
            "e/c.cfgproto": 'import "e/config.inc";\nmessage C {}\n',
            "e/config.inc": 'import "e/nested.inc";\n',
            "e/d.proto": 'syntax = "proto2";\nimport "e/opts.inc";\nmessage D {}\n',
            "e/opts.inc": 'import "e/nested.inc";\n',
            "e/nested.inc": "\n",
        })
        graph = lib.make(files, "e")

        event = lib.node_by_output(graph, "$(B)/e/a.ev.pb.h")
        self.assertEqual(event["kv"], {"p": "EV", "pc": "yellow"})
        self.assertEqual(
            event["outputs"], ["$(B)/e/a.ev.pb.cc", "$(B)/e/a.ev.pb.h"]
        )
        self.assertEqual(event["inputs"][:5], [
            "$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide",
            "$(B)/contrib/tools/protoc/protoc", "$(B)/tools/event2cpp/event2cpp",
            "$(S)/build/scripts/cpp_proto_wrapper.py", "$(S)/e/a.ev",
        ])
        self.assertEqual(set(event["inputs"][5:]), {
            "$(S)/e/b.ev", "$(S)/e/nested.inc", "$(S)/e/c.cfgproto",
            "$(S)/e/config.inc", "$(S)/e/opts.inc",
        })
        event_object = lib.node_by_output(graph, "$(B)/e/a.ev.pb.cc.o")
        for path in (
            "$(B)/e/b.ev.pb.h", "$(B)/e/c.cfgproto.pb.h", "$(S)/e/opts.inc",
            "$(S)/e/nested.inc",
        ):
            self.assertIn(path, event_object["inputs"])

        config = lib.node_by_output(graph, "$(B)/e/c.cfgproto.pb.h")
        self.assertEqual(config["inputs"][-3], "$(S)/e/c.cfgproto")
        self.assertEqual(
            set(config["inputs"][-2:]), {"$(S)/e/nested.inc", "$(S)/e/config.inc"}
        )

        proto = lib.node_by_output(graph, "$(B)/e/d.pb.h")
        self.assertEqual(proto["outputs"], ["$(B)/e/d.pb.h", "$(B)/e/d.pb.cc"])
        self.assertEqual(proto["inputs"][-3], "$(S)/e/d.proto")
        self.assertEqual(
            set(proto["inputs"][-2:]), {"$(S)/e/nested.inc", "$(S)/e/opts.inc"}
        )
        proto_object = lib.node_by_output(graph, "$(B)/e/d.pb.cc.o")
        self.assertEqual(
            {i for i in proto_object["inputs"] if "/e/" in i},
            {
                "$(B)/e/d.pb.cc", "$(B)/e/d.pb.h", "$(S)/e/nested.inc",
                "$(S)/e/opts.inc", "$(S)/e/d.proto",
            },
        )

        archive = lib.node_by_output(graph, "$(B)/e/libe.a")
        self.assertEqual([i for i in archive["inputs"] if i.endswith(".o")], [
            "$(B)/e/a.ev.pb.cc.o", "$(B)/e/b.ev.pb.cc.o",
            "$(B)/e/c.cfgproto.pb.cc.o", "$(B)/e/d.pb.cc.o",
        ])


if __name__ == "__main__":
    unittest.main(verbosity=2)
