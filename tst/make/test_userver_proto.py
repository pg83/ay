import unittest

from tst import lib
from tst.make.test_proto_cpp import empty_library, proto_files
from tst.make.test_proto_desc import desc_files


USERVER_GRPC_TOOL = "taxi/uservices/userver-arc-utils/scripts/grpc/arc-generate-service"
USERVER_STRUCTS_TOOL = "taxi/uservices/userver-arc-utils/scripts/proto_structs"


def userver_files():
    files = {**desc_files(), **proto_files()}
    for peer in ("taxi/uservices/userver/grpc", "taxi/uservices/userver/libraries/proto-structs"):
        empty_library(files, peer)
    for tool, name in ((USERVER_GRPC_TOOL, "arc-generate-service"), (USERVER_STRUCTS_TOOL, "proto_structs")):
        lib.tool_program(files, tool, name)
        files[f"{tool}/ya.make"] = files[f"{tool}/ya.make"].replace(
            "END()", "INDUCED_DEPS(h+cpp runtime/common.hpp)\nINDUCED_DEPS(cpp runtime/impl.hpp)\nEND()")
    files.update({
        "runtime/common.hpp": "#pragma once\n",
        "runtime/impl.hpp": "#pragma once\n",
        "model/ya.make": "PROTO_LIBRARY()\nPROTO_NAMESPACE(model)\nSRCS(a.proto b.proto)\nEND()\n",
        "model/a.proto": 'syntax = "proto3";\nimport "b.proto";\nmessage A {}\n',
        "model/b.proto": 'syntax = "proto3";\nmessage B {}\n',
        "desc/ya.make": "PROTO_DESCRIPTIONS()\nPEERDIR(model)\nEND()\n",
    })
    files["contrib/tools/protoc/ya.make"] = files["contrib/tools/protoc/ya.make"].replace(
        "END()", "INDUCED_DEPS(cpp runtime/protoc.hpp)\nEND()")
    files["runtime/protoc.hpp"] = ""
    return files


class UserverProtoTest(unittest.TestCase):
    def test_userver_sources_glob_suffix_and_exclusions(self):
        files = {
            "p/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "USRV_ALL_SRCS(RECURSIVE src SUFFIX _impl EXCLUDE **/*test*)\nEND()\n"),
            "p/src/a_impl.cpp": "int a;\n",
            "p/src/sub/b_impl.cc": "int b;\n",
            "p/src/sub/b_test_impl.cpp": "",
            "p/src/ignored.cpp": "",
            "p/src/header_impl.hpp": "",
        }
        graph = lib.make(files, "p")
        compiled = {n["outputs"][0] for n in graph["graph"] if n["kv"]["p"] == "CC"}
        self.assertEqual(compiled, {"$(B)/p/_/src/a_impl.cpp.o", "$(B)/p/_/src/sub/b_impl.cc.o"})

    def test_userver_grpc_dispatch_and_induced_dependencies(self):
        files = userver_files()
        files["framework/ya.make"] = (
            "LIBRARY()\nPROTO_NAMESPACE(model)\n"
            "USRV_GEN_GRPC_SERVICE_V2(FROM model model/a.proto model/b.proto)\nEND()\n")
        graph = lib.make(files, "framework")
        for name in ("a", "b"):
            src = f"$(B)/model/{name}_service.usrv.pb.cpp"
            producer = lib.node_by_output(graph, src)
            self.assertEqual(producer["outputs"], [src, f"$(B)/model/{name}_service.usrv.pb.hpp"])
            self.assertEqual(producer["kv"], {"p": "UG", "pc": "yellow"})
            self.assertEqual(producer["cmds"][0]["cwd"], "$(B)")
            args = producer["cmds"][0]["cmd_args"]
            self.assertEqual(args[-3:], [
                f"--plugin=protoc-gen-ugrpc-service=$(B)/{USERVER_GRPC_TOOL}/arc-generate-service",
                "--ugrpc-service_out=$(B)", f"model/{name}.proto",
            ])
            compiled = next(n for n in graph["graph"] if n["kv"]["p"] == "CC" and src in n["inputs"])
            self.assertIn("$(S)/runtime/common.hpp", compiled["inputs"])
            self.assertIn("$(S)/runtime/impl.hpp", compiled["inputs"])
            self.assertIn("$(S)/runtime/protoc.hpp", compiled["inputs"])
            self.assertIn(f"$(B)/model/{name}.pb.h", compiled["inputs"])

    def test_userver_structs_queries_origin_inputs_and_maps_imports(self):
        files = userver_files()
        files["framework/ya.make"] = (
            "LIBRARY()\nPROTO_NAMESPACE(model)\nPEERDIR(model)\n"
            "USRV_GEN_PROTO_STRUCTS(PROTODESC desc ORIGINS model OPTS_FILE options.json)\nEND()\n")
        files["options.json"] = "{}"
        graph = lib.make(files, "framework")
        a = lib.node_by_output(graph, "$(B)/model/a.structs.usrv.pb.hpp")
        b = lib.node_by_output(graph, "$(B)/model/b.structs.usrv.pb.hpp")
        self.assertEqual(a["kv"]["p"], "RP")
        self.assertEqual(a["outputs"], [
            "$(B)/model/a.structs.usrv.pb.hpp", "$(B)/model/a.structs.usrv.pb.cpp",
        ])
        args = a["cmds"][0]["cmd_args"]
        self.assertIn(f"--plugin=protoc-gen-uproto-structs=$(B)/{USERVER_STRUCTS_TOOL}/proto_structs", args)
        self.assertIn("--uproto-structs_out=$(B)/model", args)
        self.assertIn('--uproto-structs_opt={"opts_file": "$(S)/options.json"}', args)
        self.assertEqual(args[-1], "a.proto")
        self.assertIn("$(S)/options.json", a["inputs"])
        bundle = next(n for n in graph["graph"] if n["kv"]["p"] == "BN")
        self.assertIn(bundle["outputs"][0], a["inputs"])
        self.assertIn(bundle["uid"], a["deps"])
        compiled = next(n for n in graph["graph"] if n["kv"]["p"] == "CC"
                        and "$(B)/model/a.structs.usrv.pb.cpp" in n["inputs"])
        self.assertIn("$(B)/model/b.structs.usrv.pb.hpp", compiled["inputs"])
        self.assertIn(b["uid"], compiled["deps"])
        self.assertIn("$(S)/runtime/impl.hpp", compiled["inputs"])
        self.assertIn("$(S)/runtime/protoc.hpp", compiled["inputs"])
        self.assertIn("$(B)/model/a.structs.usrv.pb.hpp", compiled["inputs"])

    def test_grpc_structs_clients_and_servers(self):
        for side, macro in (("client", "USRV_GEN_GRPC_CLIENT_V2_STRUCTS"),
                            ("service", "USRV_GEN_GRPC_SERVICE_V2_STRUCTS")):
            with self.subTest(side=side):
                files = userver_files()
                tool = "taxi/uservices/userver-arc-utils/scripts/grpc/arc-generate-" + side
                lib.tool_program(files, tool, "arc-generate-" + side)
                empty_library(files, "taxi/uservices/userver/libraries/grpc-proto-structs")
                files["framework/ya.make"] = (
                    "LIBRARY()\nPROTO_NAMESPACE(model)\n"
                    f"{macro}(FROM model model/a.proto)\n"
                    "USRV_GEN_PROTO_STRUCTS(PROTODESC desc FILES model/a.proto model/b.proto)\nEND()\n")
                header_side = "client" if side == "client" else "server"
                for name in ("codegen_declarations.hpp", "codegen_definitions.hpp"):
                    files[f"userver/grpc-proto-structs/{header_side}/impl/{name}"] = "#pragma once\n"
                graph = lib.make(files, "framework")
                src = f"$(B)/model/a_{side}.usrv.pb.cpp"
                generated = lib.node_by_output(graph, src)
                self.assertIn(f'--ugrpc-{side}_opt={{"structs": true}}',
                              generated["cmds"][0]["cmd_args"])
                self.assertIn("$(S)/model/a.proto", generated["inputs"])
                compiled = next(n for n in graph["graph"]
                                if n["kv"]["p"] == "CC" and src in n["inputs"])
                self.assertIn(generated["uid"], compiled["deps"])

    def test_structs_explicit_output_includes(self):
        files = userver_files()
        files["framework/ya.make"] = (
            "LIBRARY()\nPROTO_NAMESPACE(model)\nPEERDIR(model)\n"
            "USRV_GEN_PROTO_STRUCTS(PROTODESC desc FILE model/a.proto "
            "OUTPUT_INCLUDES runtime/extra.hpp FILE model/b.proto)\nEND()\n")
        files["runtime/extra.hpp"] = "#pragma once\n"
        graph = lib.make(files, "framework")
        compiled = next(n for n in graph["graph"] if n["kv"]["p"] == "CC"
                        and "$(B)/model/a.structs.usrv.pb.cpp" in n["inputs"])
        self.assertIn("$(S)/runtime/extra.hpp", compiled["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
