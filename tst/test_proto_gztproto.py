import unittest

import lib


CONVERTER = "$(B)/dict/gazetteer/converter/converter"
PROTOC = "$(B)/contrib/tools/protoc/protoc"
WRAPPER = "$(S)/build/scripts/cpp_proto_wrapper.py"


def gazetteer_files(converter_induced_deps):
    files = {
        "build/scripts/cpp_proto_wrapper.py": "print('proto')\n",
        "contrib/libs/protobuf/ya.make": (
            "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
            "ADDINCL(GLOBAL contrib/libs/protobuf/src)\n"
            "SRCS(protobuf.cpp)\nEND()\n"
        ),
        "contrib/libs/protobuf/protobuf.cpp": "int protobuf(){return 0;}\n",
        "kernel/gazetteer/proto/ya.make": (
            "PROTO_LIBRARY()\nPROTO_NAMESPACE(kernel/gazetteer/proto)\n"
            "SRCS(base.proto syn.proto)\nEND()\n"
        ),
        "kernel/gazetteer/proto/base.proto": (
            'syntax = "proto2";\nmessage TArticle {}\n'
        ),
        "kernel/gazetteer/proto/syn.proto": 'syntax = "proto2";\nmessage TSyn {}\n',
        "kernel/gazetteer/rt.h": "#pragma once\n",
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
    lib.tool_program(files, "dict/gazetteer/converter", "converter")
    files["dict/gazetteer/converter/ya.make"] = files[
        "dict/gazetteer/converter/ya.make"
    ].replace("END()", f"INDUCED_DEPS(h+cpp {converter_induced_deps})\nEND()")
    return files


class ProtoGztprotoTest(unittest.TestCase):
    def test_converter_and_generated_proto_in_proto_library(self):
        files = gazetteer_files(
            "${ARCADIA_ROOT}/kernel/gazetteer/proto/base.proto "
            "${ARCADIA_ROOT}/kernel/gazetteer/rt.h "
            "${ARCADIA_ROOT}/kernel/gazetteer/proto/base.proto"
        )
        files.update({
            "g/ya.make": (
                "PROTO_LIBRARY()\nPEERDIR(kernel/gazetteer/proto)\n"
                "SRCS(a.gztproto b.gztproto)\nEND()\n"
            ),
            "g/a.gztproto": (
                'import "g/b.gztproto";\nimport "base.proto";\nmessage A {}\n'
            ),
            "g/b.gztproto": "message B {}\n",
        })
        graph = lib.make(files, "g")

        converter = lib.node_by_output(graph, "$(B)/g/a.proto")
        self.assertEqual(converter["kv"], {"p": "GZ", "pc": "yellow"})
        self.assertEqual(converter["outputs"], ["$(B)/g/a.proto"])
        self.assertEqual(converter["cmds"][0]["cmd_args"], [
            CONVERTER, "-I$(S)/contrib/libs/protobuf/src", "-I$(B)", "-I$(S)",
            "-I$(S)/kernel/gazetteer/proto", "-I$(S)",
            "$(S)/g/a.gztproto", "$(B)/g/a.proto",
        ])
        self.assertNotIn("cwd", converter["cmds"][0])
        self.assertEqual(converter["inputs"][:3], [
            CONVERTER, "$(S)/kernel/gazetteer/proto/base.proto", "$(S)/g/a.gztproto",
        ])
        self.assertEqual(set(converter["inputs"][3:]), {
            "$(S)/kernel/gazetteer/proto/base.proto", "$(S)/g/b.gztproto",
        })
        self.assertNotIn("$(S)/kernel/gazetteer/rt.h", converter["inputs"])
        self.assertIn(
            lib.node_by_output(graph, CONVERTER)["uid"], converter["deps"]
        )

        pb = lib.node_by_output(graph, "$(B)/g/a.pb.h")
        self.assertEqual(pb["kv"]["p"], "PB")
        self.assertEqual(pb["outputs"], ["$(B)/g/a.pb.h", "$(B)/g/a.pb.cc"])
        self.assertEqual(pb["cmds"][0]["cwd"], "$(B)")
        self.assertIn("g/a.proto", pb["cmds"][0]["cmd_args"])
        self.assertEqual(pb["inputs"][:5], [
            "$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide",
            PROTOC, WRAPPER, "$(B)/g/a.proto", "$(S)/g/a.gztproto",
        ])
        self.assertIn("$(B)/g/b.proto", pb["inputs"])
        self.assertIn(converter["uid"], pb["deps"])

        compile_node = lib.node_by_output(graph, "$(B)/g/a.pb.cc.o")
        for path in (
            "$(B)/g/a.pb.cc", "$(B)/g/a.pb.h", "$(B)/g/b.pb.h",
            "$(B)/kernel/gazetteer/proto/base.pb.h",
            "$(S)/g/a.gztproto", "$(S)/g/b.gztproto",
        ):
            self.assertIn(path, compile_node["inputs"])

        archive = lib.node_by_output(graph, "$(B)/g/libg.a")
        self.assertEqual(
            [i for i in archive["inputs"] if i.endswith(".o")],
            ["$(B)/g/a.pb.cc.o", "$(B)/g/b.pb.cc.o"],
        )

    def test_plain_library_with_relative_induced_protos(self):
        files = gazetteer_files(
            "kernel/gazetteer/proto/syn.proto kernel/gazetteer/rt.h "
            "kernel/gazetteer/proto/syn.proto"
        )
        files.update({
            "g/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "PEERDIR(kernel/gazetteer/proto)\nSRCS(a.gztproto)\nEND()\n"
            ),
            "g/a.gztproto": 'import "base.proto";\nmessage A {}\n',
        })
        graph = lib.make(files, "g")

        converter = lib.node_by_output(graph, "$(B)/g/a.proto")
        self.assertEqual(converter["inputs"], [
            CONVERTER, "$(S)/kernel/gazetteer/proto/syn.proto",
            "$(S)/g/a.gztproto", "$(S)/kernel/gazetteer/proto/base.proto",
        ])
        compile_node = lib.node_by_output(graph, "$(B)/g/a.pb.cc.o")
        self.assertIn("$(B)/kernel/gazetteer/proto/syn.pb.h", compile_node["inputs"])
        self.assertIn("$(B)/kernel/gazetteer/proto/base.pb.h", compile_node["inputs"])

    def test_generated_proto_import_and_root_proto_includes(self):
        files = gazetteer_files("${ARCADIA_ROOT}/kernel/gazetteer/rt.h")
        files["kernel/gazetteer/proto/ya.make"] = (
            "PROTO_LIBRARY()\n"
            "ADDINCL(GLOBAL FOR proto ${ARCADIA_ROOT}/ "
            "GLOBAL FOR proto ${ARCADIA_BUILD_ROOT}/)\n"
            "SRCS(base.proto)\nEND()\n"
        )
        files.update({
            "g/ya.make": (
                "PROTO_LIBRARY()\nPEERDIR(kernel/gazetteer/proto)\n"
                "SRCS(b.gztproto a.gztproto)\nEND()\n"
            ),
            "g/a.gztproto": 'import "g/b.proto";\nmessage A {}\n',
            "g/b.gztproto": "message B {}\n",
        })
        graph = lib.make(files, "g")

        converter = lib.node_by_output(graph, "$(B)/g/a.proto")
        self.assertEqual(converter["cmds"][0]["cmd_args"], [
            CONVERTER, "-I$(S)/contrib/libs/protobuf/src", "-I$(B)", "-I$(S)",
            "-I$(S)", "$(S)/g/a.gztproto", "$(B)/g/a.proto",
        ])
        self.assertEqual(converter["inputs"], [
            CONVERTER, "$(S)/g/a.gztproto", "$(B)/g/b.proto", "$(S)/g/b.gztproto",
        ])
        producer = lib.node_by_output(graph, "$(B)/g/b.proto")
        self.assertIn(producer["uid"], converter["deps"])

        pb = lib.node_by_output(graph, "$(B)/g/a.pb.h")
        self.assertIn("$(S)/g/a.gztproto", pb["inputs"])
        self.assertIn("$(B)/g/b.proto", pb["inputs"])
        compile_node = lib.node_by_output(graph, "$(B)/g/a.pb.cc.o")
        self.assertIn("$(B)/g/b.pb.h", compile_node["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
