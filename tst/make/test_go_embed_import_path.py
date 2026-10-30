from tst.make.lib import MakeTest


class GoEmbedImportPathTest(MakeTest):
    def test_empty_import_path_cannot_resolve_module_relative_generated_embeds(self):
        self.write("ya.make", "GO_LIBRARY(answer)\nSET(_GO_IMPORT_PATH)\nSRCS(answer.go)\nCOPY_FILE(value.txt generated/value.txt)\nGO_EMBED_BINDIR(generated FILES generated/value.txt)\nEND()\n")
        self.write("answer.go", 'package answer\nimport "embed"\n//go:embed generated/*\nvar Files embed.FS\n')
        self.write("value.txt", "42\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("generated/value.txt", result.stderr + result.stdout)
