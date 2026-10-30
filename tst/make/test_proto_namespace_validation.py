from tst.make.lib import MakeTest


class ProtoNamespaceValidationTest(MakeTest):
    def test_namespace_wrappers_require_modules_and_valid_prefixes(self):
        for arguments in ("", "IN_UNKNOWN answer_pb2", "IN_MSG answer_pb2 IN_PROTO other_pb2", "IN_MSG"):
            with self.subTest(arguments=arguments):
                self.write("ya.make", f"PY3_LIBRARY()\nPROTO_TO_NAMESPACE({arguments})\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)
