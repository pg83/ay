from tst.make.lib import MakeTest, PROJECT


class RootedProducerInputsTest(MakeTest):
    def test_configure_file_keeps_a_rooted_generated_template(self):
        self.write("ya.make", "LIBRARY()\nCOPY_FILE(config.in staged.in)\nCONFIGURE_FILE(${BINDIR}/staged.in result.txt)\nRESOURCE(result.txt /result)\nEND()\n")
        self.write("config.in", "answer = 42\n")
        graph = self.graph()
        staged = self.node(graph, "staged.in")
        configured = self.node(graph, "result.txt")
        self.assertIn(f"$(B)/{PROJECT}/staged.in", configured["inputs"])
        self.assertIn(staged["uid"], configured["deps"])

    def test_configure_file_rejects_a_rooted_template_without_a_producer(self):
        self.write("ya.make", "LIBRARY()\nCONFIGURE_FILE(${BINDIR}/missing.in result.txt)\nRESOURCE(result.txt /result)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_decimal_md5_keeps_a_rooted_generated_input(self):
        self.write("ya.make", "LIBRARY()\nCOPY_FILE(value.txt generated.txt)\nDECIMAL_MD5_LOWER_32_BITS(hash.cpp FUNCNAME e2e_hash ${BINDIR}/generated.txt)\nEND()\n")
        self.write("value.txt", "42\n")
        graph = self.graph()
        copied = self.node(graph, "generated.txt")
        generated = self.node(graph, "hash.cpp")
        self.assertIn(f"$(B)/{PROJECT}/generated.txt", generated["inputs"])
        self.assertIn(copied["uid"], generated["deps"])
