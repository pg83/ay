import json

from tst.make.lib import MakeTest, PROJECT


class UserverProtoTest(MakeTest):
    def test_empty_generator_batches_leave_the_library_unchanged(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for macro in ("USRV_GEN_PROTO_STRUCTS(PROTODESC_NAME unused.protodesc)",
                      "USRV_GEN_GRPC_CLIENT_V2()", "USRV_GEN_GRPC_SERVICE_V2_STRUCTS()"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\nSRCS(answer.cpp)\n{macro}\nEND()\n")
                graph = self.graph()
                self.node(graph, "answer.cpp.o")
                outputs = [path for node in graph["graph"] for path in node["outputs"] if f"/{PROJECT}/" in path]
                self.assertFalse(any(".usrv.pb." in path for path in outputs), outputs)

    def schema(self, files):
        self.write("schema/ya.make", f"PROTO_LIBRARY()\nPROTO_NAMESPACE(GLOBAL {PROJECT}/schema)\nSRCS(" + " ".join(files) + ")\nEND()\n")
        for name, text in files.items():
            self.write("schema/" + name, text)
        self.write("descriptions/ya.make", f"PROTO_DESCRIPTIONS()\nPEERDIR({PROJECT}/schema)\nEND()\n")

    def library(self, body):
        self.write("ya.make", f"LIBRARY()\nPROTO_NAMESPACE({PROJECT}/schema)\nADDINCL({PROJECT})\nPEERDIR({PROJECT}/schema)\n{body}\nEND()\n")

    def test_struct_options_named_descriptor_and_explicit_output_includes(self):
        self.schema({
            "base.proto": 'syntax = "proto3"; package e2e; message Base { int32 value = 1; }\n',
            "answer.proto": 'syntax = "proto3";\npackage e2e;\nimport "base.proto";\nmessage Answer { Base value = 1; }\n',
        })
        self.write("options.json", "{}\n")
        self.write("extra.h", "#pragma once\n")
        self.library(f"BUNDLE({PROJECT}/descriptions NAME combined.protodesc)\n" +
                     f"USRV_GEN_PROTO_STRUCTS(PROTODESC_NAME combined.protodesc OPTS_FILE {PROJECT}/options.json " +
                     f"FILE {PROJECT}/schema/answer.proto IMPORTS ignored.proto OUTPUT_INCLUDES extra.h " +
                     f"FILES $S/{PROJECT}/schema/base.proto)")
        graph = self.graph()
        node = self.node(graph, "schema/answer.structs.usrv.pb.hpp")
        self.assertIn(f"$(S)/{PROJECT}/options.json", node["inputs"])
        self.assertIn(f"$(B)/{PROJECT}/combined.protodesc", node["inputs"])
        options = [arg.split("=", 1)[1] for command in node["cmds"] for arg in command["cmd_args"]
                   if arg.startswith("--uproto-structs_opt=")]
        self.assertEqual(len(options), 1)
        self.assertTrue(json.loads(options[0])["opts_file"].endswith(f"/{PROJECT}/options.json"))
        compiled = next(item for item in graph["graph"] if f"$(B)/{PROJECT}/schema/answer.structs.usrv.pb.cpp" in item["inputs"]
                        and any(path.endswith(".o") for path in item["outputs"]))
        self.assertIn(f"$(S)/{PROJECT}/extra.h", compiled["inputs"])
        self.assertIn(f"$(S)/{PROJECT}/schema/base.proto", node["inputs"])
        self.node(graph, "schema/base.structs.usrv.pb.hpp")
        self.assertIn(f"$(B)/{PROJECT}/schema/base.structs.usrv.pb.hpp", compiled["inputs"])

    def test_origins_select_only_requested_peer_inputs(self):
        self.schema({"answer.proto": 'syntax = "proto3"; package e2e; message Answer {}\n'})
        self.write("unrelated/ya.make", "PROTO_LIBRARY()\nSRCS(ignored.proto)\nEND()\n")
        self.write("unrelated/ignored.proto", 'syntax = "proto3"; package e2e; message Ignored {}\n')
        self.library(f"PEERDIR({PROJECT}/unrelated)\nUSRV_GEN_PROTO_STRUCTS(PROTODESC {PROJECT}/descriptions ORIGINS {PROJECT}/schema)")
        graph = self.graph()
        self.node(graph, "schema/answer.structs.usrv.pb.hpp")
        outputs = {path for node in graph["graph"] for path in node["outputs"]}
        self.assertNotIn(f"$(B)/{PROJECT}/unrelated/ignored.structs.usrv.pb.hpp", outputs)

    def test_grpc_client_and_service_struct_modes(self):
        self.schema({"answer.proto": '''syntax = "proto3";
package e2e;
message Request {} message Response {}
service Api { rpc Get(Request) returns (Response); }
'''})
        for side, macro in (("client", "CLIENT"), ("service", "SERVICE")):
            for structs in (False, True):
                with self.subTest(side=side, structs=structs):
                    self.library(f"USRV_GEN_PROTO_STRUCTS(PROTODESC {PROJECT}/descriptions FILE {PROJECT}/schema/answer.proto)\n" +
                                 f"USRV_GEN_GRPC_{macro}_V2{'_STRUCTS' if structs else ''}({PROJECT}/schema/answer.proto)")
                    node = self.node(self.graph(), f"schema/answer_{side}.usrv.pb.hpp")
                    options = [arg.split("=", 1)[1] for command in node["cmds"] for arg in command["cmd_args"]
                               if arg.startswith(f"--ugrpc-{side}_opt=")]
                    self.assertEqual(bool(options), structs)
                    if structs:
                        self.assertTrue(json.loads(options[0])["structs"])
