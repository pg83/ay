from tst.make.lib import MakeTest


class GazetteerConverterProtoInputsTest(MakeTest):
    def test_unrooted_converter_proto_dependencies_are_rejected_as_ambiguous(self):
        relative = "dict/gazetteer/converter/ya.make"
        original = (self.root / relative).read_text()
        self.assertEqual(original.count("\nEND()"), 1)
        extra = f"{self.project_name}/extra.proto"
        header = f"{self.project_name}/native.h"
        declaration = f"\nINDUCED_DEPS(proto {extra} {header} {extra})\n"
        self.write_tree(relative, original.replace("\nEND()", declaration + "\nEND()"))
        self.write("ya.make", f"LIBRARY()\nADDINCL({self.project_name})\nSRCS(answer.gztproto extra.proto)\nEND()\n")
        self.write("answer.gztproto", "package e2e; message Answer {}\n")
        self.write("extra.proto", 'syntax = "proto2"; package e2e; message Extra {}\n')
        self.write("native.h", "#pragma once\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("INDUCED_DEPS", result.stderr)
        self.assertIn(extra, result.stderr)
