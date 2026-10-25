import unittest

from tst import lib
from tst.make.test_java import contrib
from tst.make.test_java_features import tree


GATEWAY = "vendor/github.com/grpc-ecosystem/grpc-gateway"
V1 = GATEWAY + "/protoc-gen-swagger/options"
V2 = GATEWAY + "/v2/protoc-gen-openapiv2/options"
CATALOG = "wire/catalog"


def fixture(macro="GO_GRPC_GATEWAY_V2_OPENAPI_SRCS(unused.proto)"):
    files = tree("PEERDIR(wire/entry)", module="JAVA_LIBRARY_NON_SPLIT")
    for path, resource in (
        ("contrib/java/com/google/protobuf/protobuf-java/4.33.0", 71),
        ("contrib/java/com/google/protobuf/protobuf-kotlin/4.33.0", 72),
        ("contrib/java/io/grpc/grpc-stub/1.51.0", 73),
        ("contrib/java/io/grpc/grpc-protobuf/1.51.0", 74),
        ("contrib/java/javax/annotation/javax.annotation-api/1.3.1", 75),
        ("contrib/java/io/grpc/grpc-kotlin-stub/1.3.1", 76),
    ):
        files.update(contrib(path, jar=resource))
    files.update({
        "wire/entry/ya.make": "PROTO_LIBRARY()\n" + macro + "\nSRCS(service.proto)\nEND()\n",
        "wire/entry/service.proto": 'syntax = "proto3";\nmessage Service {}\n',
        CATALOG + "/ya.make": "PROTO_LIBRARY()\nGRPC()\n"
            "IF (NOT OPENSOURCE)\nWITH_KOTLIN_GRPC()\nENDIF()\n"
            "PROTO_NAMESPACE(GLOBAL wire/catalog)\nSRCS(api/record.proto api/value.proto)\nEND()\n",
        CATALOG + "/api/record.proto": 'syntax = "proto3";\nimport "api/value.proto";\nmessage Record {}\n',
        CATALOG + "/api/value.proto": 'syntax = "proto3";\nmessage Value {}\n',
        "build/platform/java/kotlin_grpc/ya.make": lib.resource_lib("KOTLIN_GRPC", 77),
    })
    for options in (V1, V2):
        files[options + "/ya.make"] = "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO JAVA_PROTO GO_PROTO)\n" \
            "IF (JAVA_PROTO)\nPEERDIR(wire/catalog)\nSRCS(annotation.proto)\n" \
            "ELSE()\nSRCS(irrelevant.proto)\nENDIF()\nEND()\n"
        files[options + "/annotation.proto"] = 'syntax = "proto3";\nimport "wire/catalog/api/record.proto";\n'
        files[options + "/irrelevant.proto"] = 'syntax = "proto3";\n'
    lib.tool_program(files, "build/platform/java/grpc", "grpc")
    return files


def proto(graph, source):
    return lib.node_by_output(graph, "$(B)/" + source.removesuffix(".proto") + ".jsrc")


class JavaGatewayProtoPeersTest(unittest.TestCase):
    def check_catalog(self, graph, options, kotlin=False):
        record = proto(graph, CATALOG + "/api/record.proto")
        value = proto(graph, CATALOG + "/api/value.proto")
        annotation = proto(graph, options + "/annotation.proto")
        tool_path = "$(B)/build/platform/java/grpc/grpc"
        tool = lib.node_by_output(graph, tool_path)
        self.assertCountEqual(record["inputs"], [
            "$(S)/build/scripts/tared_protoc.py", tool_path,
            "$(S)/wire/catalog/api/record.proto", "$(S)/wire/catalog/api/value.proto",
        ])
        self.assertIn(tool["uid"], record["deps"])
        self.assertIn("$(S)/wire/catalog/api/record.proto", annotation["inputs"])
        self.assertIn("$(S)/wire/catalog/api/value.proto", annotation["inputs"])
        args = record["cmds"][0]["cmd_args"]
        self.assertIn("--plugin=protoc-gen-grpc_java=" + tool_path, args)
        self.assertIn("$(B)/resources/JAVA_PROTOC/bin/protoc", args)
        self.assertEqual(record["cmds"][0]["cwd"], "$(S)")
        self.assertEqual("--kotlin_out=$(B)/java_out" in args, kotlin)
        self.assertEqual("--kotlin_grpc_out=$(B)/java_out" in args, kotlin)
        if kotlin:
            self.assertEqual(record["cmds"][0]["env"]["JAVA_HOME"], "$(B)/resources/JDK17")
            self.assertEqual(record["cmds"][0]["env"]["KOTLIN_GRPC_JAR"],
                "$(B)/resources/KOTLIN_GRPC/grpc_kotlin/protoc-gen-grpc-kotlin-1.3.1.jar")
        jar = lib.node_by_output(graph, "$(B)/wire/catalog/wire-catalog.jar")
        for generated in (record, value):
            self.assertIn(generated["outputs"][0], jar["inputs"])
            self.assertIn(generated["uid"], jar["deps"])
        options_jar = next(n for n in graph["graph"] if any(
            o.startswith("$(B)/" + options + "/") and o.endswith("-sources.jar") for o in n["outputs"]))
        self.assertIn(jar["outputs"][0], options_jar["inputs"])
        self.assertIn(jar["uid"], options_jar["deps"])
        caller = lib.node_by_output(graph, "$(B)/wire/entry/wire-entry.jar")
        self.assertIn(options_jar["outputs"][0], caller["inputs"])
        self.assertIn(options_jar["uid"], caller["deps"])
        self.assertFalse(any(o.endswith((".pb.cc", ".pb.go", "_pb2.py", ".swagger.json", ".pb.gw.go"))
            for n in graph["graph"] for o in n["outputs"]))
        return caller

    def test_openapi_macro_keeps_the_declared_java_proto_peer_chain(self):
        graph = lib.make(fixture(), "app")
        self.check_catalog(graph, V2)

    def test_swagger_macro_keeps_its_own_options_peer(self):
        graph = lib.make(fixture("GO_GRPC_GATEWAY_SWAGGER_SRCS(unused.proto)"), "app")
        self.check_catalog(graph, V1)
        self.assertFalse(any(o.startswith("$(B)/" + V2 + "/") for n in graph["graph"] for o in n["outputs"]))

    def test_optional_flag_and_empty_file_list_do_not_change_declared_peer(self):
        for args in ("NO_JSON_NAMES_FOR_FIELDS unused.proto", ""):
            with self.subTest(args=args):
                self.check_catalog(lib.make(fixture(f"GO_GRPC_GATEWAY_V2_OPENAPI_SRCS({args})"), "app"), V2)

    def test_plain_gateway_macros_do_not_declare_options_peers(self):
        for name in ("GO_GRPC_GATEWAY_SRCS", "GO_GRPC_GATEWAY_V2_SRCS"):
            with self.subTest(name=name):
                graph = lib.make(fixture(name + "(unused.proto)"), "app")
                proto(graph, "wire/entry/service.proto")
                self.assertFalse(any(o.startswith(("$(B)/" + GATEWAY, "$(B)/wire/catalog/"))
                    for n in graph["graph"] for o in n["outputs"]))

    def test_excluded_options_java_variant_does_not_create_its_peers(self):
        files = fixture()
        files[V2 + "/ya.make"] = files[V2 + "/ya.make"].replace(
            "ONLY_TAGS(CPP_PROTO JAVA_PROTO GO_PROTO)", "ONLY_TAGS(CPP_PROTO GO_PROTO)")
        graph = lib.make(files, "app")
        proto(graph, "wire/entry/service.proto")
        self.assertFalse(any(o.startswith(("$(B)/" + V2, "$(B)/wire/catalog/", "$(B)/build/platform/java/grpc/"))
            for n in graph["graph"] for o in n["outputs"]))

    def test_declared_peer_obeys_the_callers_java_tag_policy(self):
        files = fixture()
        files[V2 + "/ya.make"] = "JAVA_TEST_LIBRARY(blocked)\nEND()\n"
        result = lib.make_process(files, "app")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("prohibited by incompatible tags", result.stderr)
        self.assertIn(V2, result.stderr)
        self.assertNotIn("panic:", result.stderr)

    def test_internal_catalog_context_retains_kotlin_options_and_resources(self):
        self.check_catalog(lib.make(fixture(), "app", opensource=False), V2, kotlin=True)

    def test_shared_lexical_include_keeps_each_java_module_environment(self):
        files = fixture()
        files["shared/gateway.inc"] = "IF (ENABLE_GATEWAY)\nGO_GRPC_GATEWAY_V2_OPENAPI_SRCS()\nENDIF()\n"
        for project, enabled in (("entry", "plain"), ("plain", "entry")):
            with self.subTest(project=project):
                for name in (project, enabled):
                    files[f"wire/{name}/ya.make"] = "PROTO_LIBRARY()\n" \
                        + f"SET(ENABLE_GATEWAY {'yes' if name == project else 'no'})\n" \
                        + "INCLUDE(${ARCADIA_ROOT}/shared/gateway.inc)\nSRCS(service.proto)\nEND()\n"
                    files[f"wire/{name}/service.proto"] = 'syntax = "proto3";\n'
                files["app/ya.make"] = "JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(wire/entry wire/plain)\nEND()\n"
                graph = lib.make(files, "app")
                options = proto(graph, V2 + "/annotation.proto")
                self.assertTrue(options["outputs"])
                active = lib.node_by_output(graph, f"$(B)/wire/{project}/wire-{project}.jar")
                inactive = lib.node_by_output(graph, f"$(B)/wire/{enabled}/wire-{enabled}.jar")
                self.assertTrue(any(i.startswith("$(B)/" + V2 + "/") for i in active["inputs"]))
                self.assertFalse(any(i.startswith("$(B)/" + V2 + "/") for i in inactive["inputs"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
