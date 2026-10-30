import json

from tst.make.lib import MakeTest, PROJECT


class LargeFileDescriptorsTest(MakeTest):
    def test_nested_placeholders_preserve_the_requested_output_path(self):
        self.write("ya.make", "LIBRARY()\nLARGE_FILES(nested/value.bin)\nRESOURCE(nested/value.bin /value)\nEND()\n")
        self.write("nested/value.bin.external", json.dumps({"storage": "SANDBOX", "resource_id": 123456789}))
        self.node(self.graph(), "nested/value.bin")

    def test_malformed_placeholders_are_configuration_errors(self):
        self.write("ya.make", "LIBRARY()\nLARGE_FILES(value.bin)\nRESOURCE(value.bin /value)\nEND()\n")
        for text in ("{", '{"storage":"SANDBOX"}', '{"resource_id":123456789}'):
            with self.subTest(text=text):
                self.write("value.bin.external", text)
                result = self.make()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("value.bin.external", result.stderr)

    def test_unknown_storage_kinds_are_rejected(self):
        self.write("ya.make", "LIBRARY()\nLARGE_FILES(value.bin)\nRESOURCE(value.bin /value)\nEND()\n")
        self.write("value.bin.external", json.dumps({"storage": "E2E_UNKNOWN", "resource_id": 123456789}))
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("E2E_UNKNOWN", result.stderr)

    def test_rooted_large_file_arguments_are_rejected(self):
        self.write("ya.make", f"LIBRARY()\nLARGE_FILES(${{ARCADIA_ROOT}}/{PROJECT}/value.bin)\nRESOURCE(value.bin /value)\nEND()\n")
        self.write("value.bin", "42\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_build_rooted_source_directories_are_rejected(self):
        self.write("ya.make", "LIBRARY()\nSRCDIR(${BINDIR}/generated)\nCOPY_FILE(template.txt generated/value.bin)\nLARGE_FILES(value.bin)\nRESOURCE(value.bin /value)\nEND()\n")
        self.write("template.txt", "42\n")
        self.write("value.bin.external", json.dumps({"storage": "SANDBOX", "resource_id": 123456789}))
        self.assertNotEqual(self.make().returncode, 0)

    def test_misplaced_autoupdated_keywords_do_not_hide_local_files(self):
        self.write("ya.make", "LIBRARY()\nLARGE_FILES(value.bin AUTOUPDATED)\nRESOURCE(value.bin /value)\nEND()\n")
        self.write("value.bin", "42\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/value.bin" in node["inputs"] for node in graph["graph"]))
