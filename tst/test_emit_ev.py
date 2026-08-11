import unittest

import lib


class EmitEventTest(unittest.TestCase):
    def common_files(self):
        files = {
            "build/scripts/cpp_proto_wrapper.py": "print('proto')\n",
            "contrib/libs/protobuf/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "SRCS(protobuf.cpp)\nEND()\n"
            ),
            "contrib/libs/protobuf/protobuf.cpp": "int protobuf(){return 0;}\n",
            "library/cpp/eventlog/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "SRCS(eventlog.cpp)\nEND()\n"
            ),
            "library/cpp/eventlog/eventlog.cpp": "int eventlog(){return 0;}\n",
        }
        lib.tool_program(files, "contrib/tools/protoc", "protoc")
        lib.tool_program(
            files,
            "contrib/tools/protoc/plugins/cpp_styleguide",
            "cpp_styleguide",
        )
        lib.tool_program(files, "tools/event2cpp", "event2cpp")
        for header in (
            "google/protobuf/arena.h",
            "google/protobuf/arenastring.h",
            "google/protobuf/extension_set.h",
            "google/protobuf/generated_message_reflection.h",
            "google/protobuf/generated_message_util.h",
            "google/protobuf/io/coded_stream.h",
            "google/protobuf/message.h",
            "google/protobuf/metadata_lite.h",
            "google/protobuf/port_def.inc",
            "google/protobuf/port_undef.inc",
            "google/protobuf/repeated_field.h",
            "google/protobuf/unknown_field_set.h",
            "google/protobuf/generated_message_bases.h",
            "google/protobuf/map_entry.h",
            "google/protobuf/map_entry_lite.h",
            "google/protobuf/map_field.h",
            "google/protobuf/map_field_inl.h",
            "google/protobuf/map_field_lite.h",
            "google/protobuf/reflection_ops.h",
            "google/protobuf/io/printer.h",
            "google/protobuf/io/zero_copy_sink.h",
            "google/protobuf/stubs/hash.h",
            "google/protobuf/stubs/stringpiece.h",
            "google/protobuf/stubs/strutil.h",
            "google/protobuf/wire_format.h",
        ):
            files[f"contrib/libs/protobuf/src/{header}"] = "#pragma once\n"
        files["contrib/libs/protobuf/src/google/protobuf/descriptor.proto"] = (
            'syntax = "proto2";\n'
        )
        files["contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/cleanup.h"] = (
            "#pragma once\n"
        )
        files[
            "contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/internal/cleanup.h"
        ] = "#pragma once\n"
        return files

    def event_args(self, files):
        graph = lib.make(files, "consumer")
        node = lib.node_by_output(graph, "$(B)/consumer/events.ev.pb.cc")
        self.assertIn("$(B)/consumer/events.ev.pb.h", node["outputs"])
        return node["cmds"][0]["cmd_args"]

    def test_transitive_proto_namespace_reaches_event_command_once(self):
        files = self.common_files()
        files.update({
            "leaf/ya.make": (
                "PROTO_LIBRARY()\nPROTO_NAMESPACE(yt)\n"
                "SRCS(leaf.proto)\nEND()\n"
            ),
            "leaf/leaf.proto": (
                'syntax = "proto3";\npackage test;\nmessage Leaf {}\n'
            ),
            "consumer/ya.make": (
                "PROTO_LIBRARY()\nPEERDIR(leaf)\nSRCS(events.ev)\nEND()\n"
            ),
            "consumer/events.ev": "message TEvent {\n}\n",
        })
        args = self.event_args(files)
        namespace = "-I=$(S)/yt"
        self.assertEqual(args.count(namespace), 1)
        self.assertNotIn("-I$(S)/yt", args)
        self.assertLess(args.index(namespace), args.index("--cpp_out=:$(B)/"))
        self.assertNotIn("--cpp_out=proto_h=true:$(B)/", args)

    def test_lite_headers_select_proto_h_cpp_output(self):
        files = self.common_files()
        files.update({
            "consumer/ya.make": (
                "PROTO_LIBRARY()\nSET(PROTOC_TRANSITIVE_HEADERS \"no\")\n"
                "SRCS(events.ev)\nEND()\n"
            ),
            "consumer/events.ev": "message TEvent {\n}\n",
        })
        args = self.event_args(files)
        self.assertIn("--cpp_out=proto_h=true:$(B)/", args)
        self.assertNotIn("--cpp_out=:$(B)/", args)


if __name__ == "__main__":
    unittest.main(verbosity=2)
