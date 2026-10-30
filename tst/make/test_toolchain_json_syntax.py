import json

from tst.make.lib import MakeTest


class ToolchainJsonSyntaxTest(MakeTest):
    def setUp(self):
        super().setUp()
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")

    def test_escaped_toolchain_section_names_are_decoded(self):
        config = json.loads((self.root / "build/ya.conf.json").read_text())
        text = json.dumps(config).replace('"toolchain":', r'"\u0074oolchain":', 1)
        self.write_tree("build/ya.conf.json", text)
        self.node(self.graph(), "answer.cpp.o")

    def test_malformed_object_members_are_rejected(self):
        for text in ('{', '{123: 1}', '{"toolchain" []}',
                     '{"toolchain": {}!}', '{"toolchain": {}'):
            with self.subTest(text=text):
                self.write_tree("build/ya.conf.json", text)
                self.assertNotEqual(self.make().returncode, 0)

    def test_unterminated_and_invalidly_escaped_keys_are_rejected(self):
        for text in ('{"unterminated', '{"bad\\z": 1, "toolchain": {}}'):
            with self.subTest(text=text):
                self.write_tree("build/ya.conf.json", text)
                self.assertNotEqual(self.make().returncode, 0)

    def test_truncated_values_are_rejected(self):
        for text in ('{"toolchain":', '{"toolchain": {"name":',
                     '{"toolchain": {"name": "unterminated', '{"toolchain": 123'):
            with self.subTest(text=text):
                self.write_tree("build/ya.conf.json", text)
                self.assertNotEqual(self.make().returncode, 0)
