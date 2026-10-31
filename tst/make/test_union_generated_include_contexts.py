from tst.make.lib import MakeTest, PROJECT


class UnionGeneratedIncludeContextsTest(MakeTest):
    def test_union_keeps_generated_headers_with_explicit_output_includes(self):
        self.write("ya.make", "UNION()\nFROM_SANDBOX(FILE 123456789 OUT_NOAUTO generated.h OUTPUT_INCLUDES missing.h)\nCOPY_FILE(${BINDIR}/generated.h packaged.h)\nEND()\n")
        graph = self.graph()
        producers = {}
        for name in ("generated.h", "packaged.h"):
            expected = f"$(B)/{PROJECT}/{name}"
            matches = [node for node in graph["graph"] if node["outputs"] == [expected]]
            self.assertEqual(len(matches), 1)
            producers[name] = matches[0]
        generated = producers["generated.h"]
        packaged = producers["packaged.h"]
        self.assertIn(generated["uid"], packaged["deps"])
        self.assertIn(f"$(B)/{PROJECT}/generated.h", packaged["inputs"])
