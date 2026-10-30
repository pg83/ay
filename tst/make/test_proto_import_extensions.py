from tst.make.lib import MakeTest


class ProtoImportExtensionsTest(MakeTest):
    def fixture(self, suffix):
        self.write("ya.make", f"LIBRARY()\nSRCS(answer.{suffix})\nEND()\n")
        self.write("answer." + suffix, 'syntax = "proto2";\nimport "dependency.inc";\nmessage Answer { optional Dependency value = 1; }\n')
        self.write("dependency.inc", 'syntax = "proto2";\nmessage Dependency { optional int32 value = 1; }\n')
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("dependency.pb.h", result.stderr)

    def test_non_proto_imports_without_generated_headers_are_rejected(self):
        for suffix in ("proto", "cfgproto"):
            with self.subTest(suffix=suffix):
                self.fixture(suffix)
