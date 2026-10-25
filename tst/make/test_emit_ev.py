import unittest

from tst import lib


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

    def test_event_plugins_extend_proto_and_event_commands_with_distinct_outputs(self):
        files = self.common_files()
        lib.tool_program(files, "tools/check_event", "check_event")
        lib.tool_program(files, "tools/proto_only", "proto_only")
        for tool, header in (("check_event", "event.h"), ("proto_only", "ordinary.h")):
            files[f"tools/{tool}/ya.make"] = files[f"tools/{tool}/ya.make"].replace(
                "END()", f"INDUCED_DEPS(h+cpp runtime/{header})\nEND()"
            )
            files[f"runtime/{header}"] = "#pragma once\n"
        files.update({
            "consumer/ya.make": (
                "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\n"
                "CPP_EV_PROTO_PLUGIN(check tools/check_event .checked.h DEPS support EXTRA_OUT_FLAG strict)\n"
                "CPP_PROTO_PLUGIN(proto_only tools/proto_only .extra.h)\n"
                "SRCS(message.proto first.ev second.ev)\nEND()\n"
            ),
            "consumer/message.proto": 'syntax = "proto3";\nmessage Message {}\n',
            "consumer/first.ev": "message First {}\n",
            "consumer/second.ev": "message Second {}\n",
            "support/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}CFLAGS(GLOBAL -DEVENT_SUPPORT)\nEND()\n",
        })
        graph = lib.make(files, "consumer")
        check = "$(B)/tools/check_event/check_event"
        plugin_arg = "--plugin=protoc-gen-check=" + check
        ordinary_arg = "--plugin=protoc-gen-proto_only=$(B)/tools/proto_only/proto_only"
        proto = lib.node_by_output(graph, "$(B)/consumer/message.pb.cc")
        proto_args = proto["cmds"][0]["cmd_args"]
        self.assertIn(plugin_arg, proto_args)
        self.assertIn(ordinary_arg, proto_args)
        self.assertIn("$(B)/consumer/message.checked.h", proto["outputs"])
        self.assertIn("$(B)/consumer/message.extra.h", proto["outputs"])
        plugin_uid = lib.node_by_output(graph, check)["uid"]
        for source in ("first.ev", "second.ev"):
            output = f"$(B)/consumer/{source}.pb.cc"
            event = lib.node_by_output(graph, output)
            args = event["cmds"][0]["cmd_args"]
            self.assertEqual(event["outputs"], [output, f"$(B)/consumer/{source}.pb.h"])
            self.assertEqual(args.count(plugin_arg), 1)
            self.assertIn("--check_out=$(B)/", args)
            self.assertIn("--check_opt=strict", args)
            self.assertNotIn(ordinary_arg, args)
            self.assertLess(args.index("-I=$(S)/library/cpp/eventlog"), args.index(plugin_arg))
            self.assertIn(check, event["inputs"])
            self.assertIn(plugin_uid, event["deps"])
            compiled = lib.node_by_output(graph, output + ".o")
            self.assertIn("-DEVENT_SUPPORT", compiled["cmds"][0]["cmd_args"])
            self.assertIn("$(S)/runtime/event.h", compiled["inputs"])
            self.assertNotIn("$(S)/runtime/ordinary.h", compiled["inputs"])

    def test_transitive_proto_namespace_reaches_event_command_once(self):
        files = self.common_files()
        files.update({
            "leaf/ya.make": (
                "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\nPROTO_NAMESPACE(yt)\n"
                "SRCS(leaf.proto)\nEND()\n"
            ),
            "leaf/leaf.proto": (
                'syntax = "proto3";\npackage test;\nmessage Leaf {}\n'
            ),
            "consumer/ya.make": (
                "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\nPEERDIR(leaf)\nSRCS(events.ev)\nEND()\n"
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
                "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\nSET(PROTOC_TRANSITIVE_HEADERS \"no\")\n"
                "SRCS(events.ev)\nEND()\n"
            ),
            "consumer/events.ev": "message TEvent {\n}\n",
        })
        args = self.event_args(files)
        self.assertIn("--cpp_out=proto_h=true:$(B)/", args)
        self.assertNotIn("--cpp_out=:$(B)/", args)

    def test_consumer_of_event_header_gets_the_producer_main_output(self):
        # devtools_ymake_bin reference: every compile including
        # devtools/ymake/diag/common_msg/msg.ev.pb.h also lists msg.ev.pb.cc,
        # the first (main) output of the event PB node.
        files = self.common_files()
        files.update({
            "consumer/ya.make": "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\nSRCS(events.ev)\nEND()\n",
            "consumer/events.ev": "message TEvent {\n}\n",
            "user/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nPEERDIR(consumer)\n"
                "SRCS(u.cpp)\nEND()\n"
            ),
            "user/u.cpp": '#include "consumer/events.ev.pb.h"\n',
        })
        graph = lib.make(files, "user")
        inputs = lib.node_by_output(graph, "$(B)/user/u.cpp.o")["inputs"]
        self.assertIn("$(B)/consumer/events.ev.pb.h", inputs)
        self.assertIn("$(B)/consumer/events.ev.pb.cc", inputs)
        # Only the path of the main output: its own includes stay out
        # (export_json.cpp FillRegularInputs renders NodeDeps by path; 106 of
        # the 107 msg.ev.pb.h compiles there lack google/protobuf/wire_format.h).
        self.assertNotIn("$(S)/contrib/libs/protobuf/src/google/protobuf/wire_format.h", inputs)


if __name__ == "__main__":
    unittest.main(verbosity=2)
