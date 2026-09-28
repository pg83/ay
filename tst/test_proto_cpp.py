import unittest

import lib


PROTOC = "$(B)/contrib/tools/protoc/protoc"
STYLEGUIDE = "$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide"
GRPC_CPP = "$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp"
WRAPPER = "$(S)/build/scripts/cpp_proto_wrapper.py"
PB_RUNTIME = "$(S)/contrib/libs/protobuf/src/google/protobuf"
EMPTY_LIBRARY = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(x.cpp)\nEND()\n"


def empty_library(files, path):
    files[f"{path}/ya.make"] = EMPTY_LIBRARY
    files[f"{path}/x.cpp"] = "int x(){return 0;}\n"


def proto_files():
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
    }
    for header in (
        "arena.h", "arenastring.h", "extension_set.h",
        "generated_message_reflection.h", "generated_message_util.h",
        "io/coded_stream.h", "message.h", "metadata_lite.h", "port_def.inc",
        "port_undef.inc", "repeated_field.h", "unknown_field_set.h",
        "generated_message_bases.h", "map_entry.h", "map_entry_lite.h",
        "map_field.h", "map_field_inl.h", "map_field_lite.h",
        "reflection_ops.h", "descriptor.pb.h",
    ):
        files[f"contrib/libs/protobuf/src/google/protobuf/{header}"] = "#pragma once\n"
    lib.tool_program(files, "contrib/tools/protoc", "protoc")
    lib.tool_program(
        files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide"
    )
    return files


def grpc_files(files):
    lib.tool_program(files, "contrib/tools/protoc/plugins/grpc_cpp", "grpc_cpp")
    empty_library(files, "contrib/libs/grpc")


def yaff_files(files):
    lib.tool_program(files, "library/cpp/yaff/tools/protoc_plugin", "protoc_plugin")
    for header in (
        "yaff.h", "struct.h", "protobuf.h", "reflect.h",
        "experiments/serializer.h", "experiments/column.h",
        "experiments/merge.h",
    ):
        files[f"library/cpp/yaff/{header}"] = "#pragma once\n"


def pb_node(graph, pb_h):
    node = lib.node_by_output(graph, pb_h)
    assert node["kv"]["p"] == "PB", node["kv"]
    return node


class ProtoCppTest(unittest.TestCase):
    def test_grpc_imports_and_named_archive(self):
        files = proto_files()
        grpc_files(files)
        files.update({
            "p/ya.make": (
                "PROTO_LIBRARY(named)\nGRPC()\n"
                "SRCS(a.proto ./b.proto sub/../c.proto d.proto)\nEND()\n"
            ),
            "p/a.proto": (
                'syntax = "proto3";\n'
                '  import "p/b.proto";\n'
                'import public "google/protobuf/descriptor.proto";\n'
                "import weak 'p/c.proto'; // trailing comment\n"
                'importx "p/ident_continuation.proto";\n'
                'import "p//comment_in_target.proto";\n'
                "import;\n"
                "\t\n"
                "int32 x = 1;\n"
                'import "./p/d.proto";\n'
                "message A {}\n"
            ),
            "p/b.proto": 'syntax = "proto3";\nmessage B {}\n',
            "p/c.proto": 'syntax = "proto3";\nmessage C {}\n',
            "p/d.proto": 'syntax = "proto3";\nmessage D {}\n',
            "c/ya.make": (
                "PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "PEERDIR(p)\nSRCS(c.cpp)\nEND()\n"
            ),
            "c/c.cpp": (
                "#include <p/a.pb.h>\n#include <p/a.grpc.pb.h>\n"
                "int main(){return 0;}\n"
            ),
        })
        graph = lib.make(files, "c")

        node = pb_node(graph, "$(B)/p/a.pb.h")
        outputs = [
            "$(B)/p/a.pb.h", "$(B)/p/a.pb.cc",
            "$(B)/p/a.grpc.pb.cc", "$(B)/p/a.grpc.pb.h",
        ]
        self.assertEqual(node["outputs"], outputs)
        self.assertEqual(node["kv"]["pc"], "yellow")
        self.assertEqual(node["cmds"][0]["cwd"], "$(S)")
        self.assertEqual(node["cmds"][0]["cmd_args"], [
            "", WRAPPER, "--outputs", *outputs, "--",
            PROTOC, "-I=./", "-I=$(S)/", "-I=$(B)", "-I=$(S)",
            "-I=$(B)", "-I=$(S)/contrib/libs/protobuf/src",
            "--cpp_out=:$(B)/", "--cpp_styleguide_out=:$(B)/",
            f"--plugin=protoc-gen-cpp_styleguide={STYLEGUIDE}",
            "p/a.proto",
            f"--plugin=protoc-gen-grpc_cpp={GRPC_CPP}",
            "--grpc_cpp_out=$(B)/",
        ])
        self.assertEqual(node["inputs"][:5], [
            STYLEGUIDE, GRPC_CPP, PROTOC, WRAPPER, "$(S)/p/a.proto",
        ])
        self.assertEqual(set(node["inputs"][5:]), {
            "$(S)/p/b.proto", "$(S)/p/c.proto", "$(S)/p/d.proto",
            f"{PB_RUNTIME}/descriptor.proto",
        })
        self.assertEqual(len(node["inputs"]), 9)
        for tool in (PROTOC, STYLEGUIDE, GRPC_CPP):
            self.assertIn(lib.node_by_output(graph, tool)["uid"], node["deps"])

        for name in ("b", "c", "d"):
            other = pb_node(graph, f"$(B)/p/{name}.pb.h")
            self.assertIn(f"p/{name}.proto", other["cmds"][0]["cmd_args"])

        archive = lib.node_by_output(graph, "$(B)/p/libnamed.a")
        self.assertIn("$(B)/p/a.grpc.pb.cc.o", archive["inputs"])

        grpc_object = lib.node_by_output(graph, "$(B)/p/a.grpc.pb.cc.o")
        self.assertIn("$(B)/p/a.pb.h", grpc_object["inputs"])
        self.assertIn(WRAPPER, grpc_object["inputs"])

        consumer = lib.node_by_output(graph, "$(B)/c/c.cpp.o")
        for path in (
            "$(B)/p/a.pb.h", "$(B)/p/b.pb.h", "$(B)/p/c.pb.h",
            "$(B)/p/d.pb.h", "$(B)/p/a.grpc.pb.h",
            f"{PB_RUNTIME}/port_def.inc", f"{PB_RUNTIME}/descriptor.pb.h",
        ):
            self.assertIn(path, consumer["inputs"])
        self.assertFalse(
            [i for i in consumer["inputs"] if "comment_in_target" in i
             or "ident_continuation" in i]
        )

    def test_plugins_yaff_lite_headers_and_namespace(self):
        files = proto_files()
        yaff_files(files)
        lib.tool_program(files, "tools/p0", "p0")
        lib.tool_program(files, "tools/p1", "p1")
        lib.tool_program(files, "tools/p2", "p2")
        files["tools/p2/ya.make"] = files["tools/p2/ya.make"].replace(
            "END()", "INDUCED_DEPS(cpp ${ARCADIA_ROOT}/rt/two_runtime.h)\nEND()"
        )
        files["rt/two_runtime.h"] = "#pragma once\n"
        empty_library(files, "deps/d0")
        empty_library(files, "deps/d2")
        files.update({
            "p/ya.make": (
                "PROTO_LIBRARY()\n"
                "SET(PROTOC_TRANSITIVE_HEADERS \"no\")\n"
                "PROTO_NAMESPACE(p)\n"
                "SET_APPEND(_PROTOC_FLAGS --fatal_warnings)\n"
                "CPP_PROTO_PLUGIN0(zero tools/p0 DEPS deps/d0)\n"
                "CPP_PROTO_PLUGIN(one tools/p1 .one.h EXTRA_OUT_FLAG a=1,,b=2)\n"
                "CPP_PROTO_PLUGIN2(two tools/p2 .two.h .two.cpp DEPS deps/d2)\n"
                "YAFF(NAMESPACE my_ns FILES a.proto EXPERIMENTAL a.proto)\n"
                "SRCS(a.proto b.proto)\nEND()\n"
            ),
            "p/a.proto": 'syntax = "proto3";\nimport "b.proto";\nmessage A {}\n',
            "p/b.proto": 'syntax = "proto3";\nmessage B {}\n',
        })
        graph = lib.make(files, "p")

        node = pb_node(graph, "$(B)/p/a.pb.h")
        outputs = [
            "$(B)/p/a.pb.h", "$(B)/p/a.pb.cc", "$(B)/p/a.deps.pb.h",
            "$(B)/p/a.one.h", "$(B)/p/a.two.h", "$(B)/p/a.two.cpp",
            "$(B)/p/a.yaff.h", "$(B)/p/a.yaff.cpp",
        ]
        self.assertEqual(node["outputs"], outputs)
        yaff_plugin = "$(B)/library/cpp/yaff/tools/protoc_plugin/protoc_plugin"
        self.assertEqual(node["cmds"][0]["cmd_args"], [
            "", WRAPPER, "--outputs", *outputs, "--",
            PROTOC, "-I=./p", "-I=$(S)/p", "-I=$(B)", "-I=$(S)", "-I=$(S)/p",
            "-I=$(B)", "-I=$(S)/contrib/libs/protobuf/src",
            "--cpp_out=proto_h=true:$(B)/p", "--fatal_warnings",
            "--cpp_styleguide_out=:$(B)/p",
            f"--plugin=protoc-gen-cpp_styleguide={STYLEGUIDE}",
            "p/a.proto",
            "--plugin=protoc-gen-zero=$(B)/tools/p0/p0", "--zero_out=$(B)/p",
            "--plugin=protoc-gen-one=$(B)/tools/p1/p1", "--one_out=$(B)/p",
            "--one_opt=a=1", "--one_opt=b=2",
            "--plugin=protoc-gen-two=$(B)/tools/p2/p2", "--two_out=$(B)/p",
            f"--plugin=protoc-gen-yaff={yaff_plugin}", "--yaff_out=$(B)/p",
            "--yaff_opt=namespace=my_ns", "--yaff_opt=file=a.proto",
            "--yaff_opt=experimental=a.proto",
        ])
        self.assertEqual(node["inputs"], [
            STYLEGUIDE, PROTOC, "$(B)/tools/p0/p0", "$(B)/tools/p1/p1",
            "$(B)/tools/p2/p2", yaff_plugin, WRAPPER,
            "$(S)/p/a.proto", "$(S)/p/b.proto",
        ])

        pb_object = lib.node_by_output(graph, "$(B)/p/a.pb.cc.o")
        self.assertIn("$(B)/p/b.pb.h", pb_object["inputs"])
        self.assertIn("$(S)/rt/two_runtime.h", pb_object["inputs"])

        yaff_a = lib.node_by_output(graph, "$(B)/p/a.yaff.cpp.o")
        for header in ("yaff.h", "experiments/merge.h"):
            self.assertIn(f"$(S)/library/cpp/yaff/{header}", yaff_a["inputs"])
        self.assertIn("$(B)/p/a.pb.h", yaff_a["inputs"])
        yaff_b = lib.node_by_output(graph, "$(B)/p/b.yaff.cpp.o")
        self.assertNotIn("$(S)/library/cpp/yaff/yaff.h", yaff_b["inputs"])

        two = lib.node_by_output(graph, "$(B)/p/a.two.cpp.o")
        self.assertIn("$(S)/rt/two_runtime.h", two["inputs"])

        archive = lib.node_by_output(graph, "$(B)/p/libp.a")
        self.assertEqual(
            [i for i in archive["inputs"] if i.endswith(".o")],
            [
                "$(B)/p/a.pb.cc.o", "$(B)/p/a.two.cpp.o", "$(B)/p/a.yaff.cpp.o",
                "$(B)/p/b.pb.cc.o", "$(B)/p/b.two.cpp.o", "$(B)/p/b.yaff.cpp.o",
            ],
        )

    def test_yaff_declared_before_lite_headers_precedes_cpp_outputs(self):
        files = proto_files()
        yaff_files(files)
        files.update({
            "p/ya.make": (
                "PROTO_LIBRARY()\n"
                "YAFF(NAMESPACE my_ns)\n"
                "SET(PROTOC_TRANSITIVE_HEADERS \"no\")\n"
                "SRCS(a.proto)\nEND()\n"
            ),
            "p/a.proto": 'syntax = "proto3";\nmessage A {}\n',
        })
        graph = lib.make(files, "p")
        node = pb_node(graph, "$(B)/p/a.pb.h")
        self.assertEqual(node["outputs"], [
            "$(B)/p/a.pb.h", "$(B)/p/a.yaff.h", "$(B)/p/a.yaff.cpp",
            "$(B)/p/a.pb.cc", "$(B)/p/a.deps.pb.h",
        ])

    def test_plugin_emitting_grpc_suffixes_without_grpc_macro(self):
        files = proto_files()
        lib.tool_program(files, "tools/gen", "gen")
        files.update({
            "p/ya.make": (
                "PROTO_LIBRARY()\n"
                "DEFAULT(PROTOC_TRANSITIVE_HEADERS no)\n"
                "PROTO_NAMESPACE(.)\n"
                "CPP_PROTO_PLUGIN2(rpc tools/gen .grpc.pb.h .grpc.pb.cc)\n"
                "SRCS(a.proto b.proto)\nEND()\n"
            ),
            "p/a.proto": 'syntax = "proto3";\nimport "p/b.proto";\nmessage A {}\n',
            "p/b.proto": 'syntax = "proto3";\nmessage B {}\n',
        })
        graph = lib.make(files, "p")
        node = pb_node(graph, "$(B)/p/a.pb.h")
        self.assertEqual(node["outputs"], [
            "$(B)/p/a.pb.h", "$(B)/p/a.pb.cc", "$(B)/p/a.deps.pb.h",
            "$(B)/p/a.grpc.pb.h", "$(B)/p/a.grpc.pb.cc",
        ])
        args = node["cmds"][0]["cmd_args"]
        self.assertIn("-I=./", args)
        self.assertIn("--cpp_out=proto_h=true:$(B)/", args)
        self.assertEqual(args[-2:], [
            "--plugin=protoc-gen-rpc=$(B)/tools/gen/gen", "--rpc_out=$(B)/",
        ])
        rpc_object = lib.node_by_output(graph, "$(B)/p/a.grpc.pb.cc.o")
        self.assertIn("$(B)/p/a.pb.h", rpc_object["inputs"])
        self.assertIn(WRAPPER, rpc_object["inputs"])
        deps_consumer = lib.node_by_output(graph, "$(B)/p/a.pb.cc.o")
        self.assertIn("$(B)/p/b.pb.h", deps_consumer["inputs"])

    def test_source_path_cache_eviction_keeps_every_proto(self):
        count = 70
        files = proto_files()
        names = [f"m{i:02d}.proto" for i in range(count)]
        files["p/ya.make"] = (
            "PROTO_LIBRARY()\nSRCS(\n" + "\n".join(names) + "\n)\nEND()\n"
        )
        for name in names:
            files[f"p/{name}"] = 'syntax = "proto3";\n'
        graph = lib.make(files, "p")
        pb_nodes = [n for n in graph["graph"] if n["kv"].get("p") == "PB"]
        self.assertEqual(len(pb_nodes), count)
        archive = lib.node_by_output(graph, "$(B)/p/libp.a")
        objects = [i for i in archive["inputs"] if i.endswith(".o")]
        self.assertEqual(
            objects, [f"$(B)/p/m{i:02d}.pb.cc.o" for i in range(count)]
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
