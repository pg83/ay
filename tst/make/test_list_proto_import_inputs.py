from tst.make.lib import MakeTest


class ListProtoImportInputsTest(MakeTest):
    def test_proto_lists_keep_imported_source_dependencies(self):
        project = self.project_name
        self.write("ya.make", f"LIBRARY()\nPEERDIR({project}/proto)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", f'#include "{project}/proto/files.pb.h"\nint answer() {{ return 42; }}\n')
        self.write("proto/ya.make", "PROTO_LIBRARY()\nSRCS(answer.proto dependency.proto)\nLIST_PROTO(TO files.proto answer.proto)\nEND()\n")
        self.write("proto/answer.proto", f'syntax = "proto2";\nimport "{project}/proto/dependency.proto";\npackage e2e;\nmessage Answer {{ optional Dependency value = 1; }}\n')
        self.write("proto/dependency.proto", 'syntax = "proto2";\npackage e2e;\nmessage Dependency {}\n')
        listed = self.node(self.graph(), "proto/files.proto")
        self.assertIn(f"$(S)/{project}/proto/answer.proto", listed["inputs"])
        self.assertIn(f"$(S)/{project}/proto/dependency.proto", listed["inputs"])
