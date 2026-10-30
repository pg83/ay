import io
import tarfile

from tst.make.lib import MakeTest, PROJECT


class GeneratedGettextTest(MakeTest):
    def test_missing_translation_sources_are_rejected(self):
        self.write("ya.make", "LIBRARY()\nSRCS(missing.po)\nRESOURCE(missing.mo /translations)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def check_translation(self, graph):
        source = self.node(graph, "answer.po")
        translated = self.node(graph, "answer.mo")
        self.assertIn(f"$(B)/{PROJECT}/answer.po", translated["inputs"])
        self.assertIn(source["uid"], translated["deps"])

    def test_archived_translation_sources_keep_their_extraction_dependency(self):
        self.write("ya.make", "LIBRARY()\nFROM_ARCHIVE(sources.tar OUT answer.po)\nRESOURCE(answer.mo /translations)\nEND()\n")
        value = b'msgid "answer"\nmsgstr "42"\n'
        with tarfile.open(self.project / "sources.tar", "w") as archive:
            entry = tarfile.TarInfo("answer.po")
            entry.size = len(value)
            archive.addfile(entry, io.BytesIO(value))
        self.check_translation(self.graph())

    def test_declared_generated_translations_keep_their_copy_dependency(self):
        self.write("ya.make", "LIBRARY()\nCOPY_FILE(template.po answer.po)\nSRCS(answer.po)\nRESOURCE(answer.mo /translations)\nEND()\n")
        self.write("template.po", 'msgid "answer"\nmsgstr "42"\n')
        self.check_translation(self.graph())
