from tst.make.lib import MakeTest


class UnionGeneratedSourcesTest(MakeTest):
    def test_redeclaring_a_generated_source_as_a_rooted_source_is_rejected(self):
        self.write("ya.make", "UNION(answer)\nRUN_PYTHON3(generate.py STDOUT_NOAUTO generated.txt)\nSRCS(${BINDIR}/generated.txt)\nEND()\n")
        self.write("generate.py", "print(42)\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("generated.txt", result.stderr)

    def test_disabling_peer_passing_keeps_own_files(self):
        self.write("ya.make", "UNION(answer)\nSET(PASS_PEERS no)\nFILES(value.txt)\nEND()\n")
        self.write("value.txt", "42\n")
        graph = self.graph()
        output = f"$(B)/{self.project_name}/value.txt"
        source = f"$(S)/{self.project_name}/value.txt"
        copied = [node for node in graph["graph"] if output in node["outputs"] and source in node["inputs"]]
        self.assertEqual(len(copied), 1)
        self.assertIn(output, self.node(graph, "answer.pkg.fake")["outputs"])
