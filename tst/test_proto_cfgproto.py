import unittest

import lib


PROTOC = "$(B)/contrib/tools/protoc/protoc"
STYLEGUIDE = "$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide"
CONFIG_PLUGIN = "$(B)/library/cpp/proto_config/plugin/plugin"
WRAPPER = "$(S)/build/scripts/cpp_proto_wrapper.py"
PB_RUNTIME = "$(S)/contrib/libs/protobuf/src/google/protobuf"


def cfgproto_files():
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
        "library/cpp/proto_config/codegen/ya.make": (
            "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(x.cpp)\nEND()\n"
        ),
        "library/cpp/proto_config/codegen/x.cpp": "int x(){return 0;}\n",
        "library/cpp/proto_config/codegen/codegen.h": "#pragma once\n",
        "library/cpp/proto_config/protos/ya.make": (
            "PROTO_LIBRARY()\nSRCS(extensions.proto)\nEND()\n"
        ),
        "library/cpp/proto_config/protos/extensions.proto": 'syntax = "proto2";\n',
    }
    for header in (
        "generated_message_bases.h", "map_entry.h", "map_entry_lite.h",
        "map_field.h", "map_field_inl.h", "map_field_lite.h",
        "reflection_ops.h", "descriptor.pb.h",
    ):
        files[f"contrib/libs/protobuf/src/google/protobuf/{header}"] = "#pragma once\n"
    lib.tool_program(files, "contrib/tools/protoc", "protoc")
    lib.tool_program(
        files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide"
    )
    lib.tool_program(files, "library/cpp/proto_config/plugin", "plugin")
    return files


class ProtoCfgprotoTest(unittest.TestCase):
    def test_config_plugin_and_include_options(self):
        files = cfgproto_files()
        files.update({
            "c/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "PROTO_NAMESPACE(c)\n"
                "PEERDIR(library/cpp/proto_config/codegen)\n"
                "SRCS(a.cfgproto b.proto use.cpp)\nEND()\n"
            ),
            "c/a.cfgproto": (
                'import "b.proto";\n'
                'import "google/protobuf/descriptor.proto";\n'
                "option (NProtoConfig.Include) = "
                '"library/cpp/proto_config/codegen/codegen.h"; // comment\n'
                '  option (NProtoConfig.Include) = "c/extra.h";\n'
                'option java_package = "c/java_package.h";\n'
                'option (NProtoConfig.Include) "c/no_equals.h";\n'
                "option (NProtoConfig.Include) = c/no_quote.h;\n"
                'option (NProtoConfig.Include) = "c/unterminated.h;\n'
                "option (NProtoConfig.Include)\n"
                "message A {}\n"
            ),
            "c/extra.h": "#pragma once\n",
            "c/b.proto": 'syntax = "proto2";\nmessage B {}\n',
            "c/use.cpp": '#include "a.cfgproto.pb.h"\nint u(){return 0;}\n',
        })
        rejected = ("java_package", "no_equals", "no_quote", "unterminated")
        for name in rejected:
            files[f"c/{name}.h"] = "#pragma once\n"
        graph = lib.make(files, "c")

        node = lib.node_by_output(graph, "$(B)/c/a.cfgproto.pb.h")
        outputs = ["$(B)/c/a.cfgproto.pb.cc", "$(B)/c/a.cfgproto.pb.h"]
        self.assertEqual(node["kv"], {"p": "PB", "pc": "yellow"})
        self.assertEqual(node["outputs"], outputs)
        self.assertEqual(node["cmds"][0]["cmd_args"], [
            "", WRAPPER, "--outputs", *outputs, "--",
            PROTOC, "-I=./c", "-I=$(S)/c", "-I=$(B)", "-I=$(S)", "-I=$(S)/c",
            "-I=$(B)", "-I=$(S)/contrib/libs/protobuf/src",
            "--cpp_out=:$(B)/c", "--cpp_styleguide_out=:$(B)/c",
            f"--plugin=protoc-gen-cpp_styleguide={STYLEGUIDE}",
            "c/a.cfgproto",
            f"--plugin=protoc-gen-config={CONFIG_PLUGIN}", "--config_out=$(B)/",
        ])
        self.assertEqual(node["inputs"], [
            STYLEGUIDE, PROTOC, CONFIG_PLUGIN, WRAPPER, "$(S)/c/a.cfgproto",
            "$(S)/c/b.proto", f"{PB_RUNTIME}/descriptor.proto",
        ])
        self.assertIn(lib.node_by_output(graph, CONFIG_PLUGIN)["uid"], node["deps"])

        compile_node = lib.node_by_output(graph, "$(B)/c/a.cfgproto.pb.cc.o")
        for path in (
            "$(B)/c/a.cfgproto.pb.cc", "$(B)/c/b.pb.h", "$(S)/c/extra.h",
            "$(S)/library/cpp/proto_config/codegen/codegen.h",
            f"{PB_RUNTIME}/descriptor.pb.h", "$(S)/c/a.cfgproto", WRAPPER,
        ):
            self.assertIn(path, compile_node["inputs"])
        use = lib.node_by_output(graph, "$(B)/c/use.cpp.o")
        self.assertIn("$(B)/c/a.cfgproto.pb.h", use["inputs"])
        self.assertIn("$(S)/c/extra.h", use["inputs"])
        for name in rejected:
            self.assertNotIn(f"$(S)/c/{name}.h", use["inputs"])

        archive = lib.node_by_output(graph, "$(B)/c/libc.a")
        self.assertEqual([i for i in archive["inputs"] if i.endswith(".o")], [
            "$(B)/c/use.cpp.o", "$(B)/c/a.cfgproto.pb.cc.o", "$(B)/c/b.pb.cc.o",
        ])

    def test_imports_without_proto_namespace_keep_source_relative_headers(self):
        files = cfgproto_files()
        files.update({
            "c/ya.make": (
                "PROTO_LIBRARY()\nSRCS(a.cfgproto b.proto)\nEND()\n"
            ),
            "c/a.cfgproto": 'import "c/b.proto";\nmessage A {}\n',
            "c/b.proto": 'syntax = "proto2";\nmessage B {}\n',
        })
        graph = lib.make(files, "c")
        node = lib.node_by_output(graph, "$(B)/c/a.cfgproto.pb.h")
        self.assertIn("--cpp_out=:$(B)/", node["cmds"][0]["cmd_args"])
        compile_node = lib.node_by_output(graph, "$(B)/c/a.cfgproto.pb.cc.o")
        self.assertIn("$(B)/c/b.pb.h", compile_node["inputs"])
        self.assertIn("$(S)/c/b.proto", compile_node["inputs"])

    def test_config_imports_and_opaque_files_parsed_as_cfgproto(self):
        files = cfgproto_files()
        files.update({
            "c/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "SRCS(base.cfgproto a.cfgproto)\nEND()\n"
            ),
            "c/base.cfgproto": "message Base {}\n",
            "c/a.cfgproto": (
                'import "c/base.cfgproto";\nimport "c/defaults.inc";\n'
                "message A {}\n"
            ),
            "c/defaults.inc": (
                'import "c/nested.inc";\n'
                'option (NProtoConfig.Include) = "c/from_defaults.h";\n'
            ),
            "c/nested.inc": "\n",
            "c/from_defaults.h": "#pragma once\n",
        })
        graph = lib.make(files, "c")
        node = lib.node_by_output(graph, "$(B)/c/a.cfgproto.pb.h")
        self.assertEqual(node["inputs"][:5], [
            STYLEGUIDE, PROTOC, CONFIG_PLUGIN, WRAPPER, "$(S)/c/a.cfgproto",
        ])
        self.assertEqual(set(node["inputs"][5:]), {
            "$(S)/c/base.cfgproto", "$(S)/c/defaults.inc", "$(S)/c/nested.inc",
        })
        compile_node = lib.node_by_output(graph, "$(B)/c/a.cfgproto.pb.cc.o")
        self.assertIn("$(B)/c/base.cfgproto.pb.h", compile_node["inputs"])
        self.assertIn("$(S)/c/defaults.inc", compile_node["inputs"])
        self.assertNotIn("$(S)/c/from_defaults.h", compile_node["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
