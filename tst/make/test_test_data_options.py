import json

from tst.make.lib import MakeTest, PROJECT


class TestDataOptionsTest(MakeTest):
    def test_resource_aliases_share_the_same_sandbox_download(self):
        self.python_test("DATA(sbr://123456789=first sbr://123456789=second)")
        graph = self.graph("-t")
        downloads = [node for node in graph["graph"] if "$(B)/sandbox-storage/123456789/resource" in node["outputs"]]
        self.assertEqual(len(downloads), 1)
        checked = self.node(graph, "test-results/py3test/meta.json")
        self.assertIn(downloads[0]["uid"], checked["deps"])
        args = [arg for command in checked["cmds"] for arg in command["cmd_args"]]
        for alias in ("first", "second"):
            self.assertIn("123456789=" + alias, args)

    def python_test(self, macros=""):
        self.write("ya.make", f"PY3TEST()\nTEST_SRCS(test_answer.py)\n{macros}\nEND()\n")
        self.write("test_answer.py", "def test_answer(): assert True\n")

    def fuzz_test(self):
        self.write("ya.make", "FUZZ()\nSRCS(fuzz.cpp)\nDATA(sbr://444444444=data)\nEND()\n")
        self.write("fuzz.cpp", '#include <stddef.h>\n#include <stdint.h>\nextern "C" int LLVMFuzzerTestOneInput(const uint8_t*, size_t) { return 0; }\n')

    def fuzz_commands(self, graph):
        commands = [command["cmd_args"] for node in graph["graph"] for command in node["cmds"] if "run_fuzz" in command["cmd_args"]]
        self.assertTrue(commands)
        return commands

    def test_local_external_data_is_copied_once_for_multiple_suites(self):
        self.write("ya.make", "RECURSE(first second)\n")
        self.write("shared.bin", "42\n")
        for directory in ("first", "second"):
            self.write(f"{directory}/ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nDATA(ext:../shared.bin)\nEND()\n")
            self.write(f"{directory}/test_answer.py", "def test_answer(): assert True\n")
        graph = self.graph("-t")
        output = f"$(B)/external_local/{PROJECT}/shared.bin"
        copies = [node for node in graph["graph"] if output in node["outputs"]]
        self.assertEqual(len(copies), 1)
        self.assertIn(f"$(S)/{PROJECT}/shared.bin", copies[0]["inputs"])
        for directory in ("first", "second"):
            self.assertIn(copies[0]["uid"], self.node(graph, f"{directory}/test-results/py3test/meta.json")["deps"])

    def test_fuzz_corpus_can_be_read_from_an_absolute_path(self):
        self.fuzz_test()
        corpus = self.work / "corpus.json"
        corpus.write_text(json.dumps({"seeds": [123456789]}))
        for args in self.fuzz_commands(self.graph("-t", env={"YA_TEST_CORPUS_DATA_PATH": str(corpus)})):
            self.assertIn("123456789=seeds/0", args)

    def test_missing_absolute_fuzz_corpus_keeps_explicit_data(self):
        self.fuzz_test()
        for args in self.fuzz_commands(self.graph("-t", env={"YA_TEST_CORPUS_DATA_PATH": str(self.work / "missing.json")})):
            self.assertEqual(["444444444=data"], [args[i + 1] for i, arg in enumerate(args) if arg == "--sandbox-resource"])

    def test_non_object_fuzz_corpus_does_not_abort_graph_generation(self):
        self.fuzz_test()
        corpus = self.work / "corpus.json"
        for value in ([], None, 42):
            with self.subTest(value=value):
                corpus.write_text(json.dumps(value))
                result = self.make("-t", env={"YA_TEST_CORPUS_DATA_PATH": str(corpus)})
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIsInstance(json.loads(result.stdout)["graph"], list)

    def test_autoupdated_test_data_keeps_the_resource_identifier(self):
        self.python_test("DATA(sbr://123456789=answer AUTOUPDATED update.py)")
        self.write("update.py", "print(123456789)\n")
        args = [arg for command in self.node(self.graph("-t"), "test-results/py3test/meta.json")["cmds"] for arg in command["cmd_args"]]
        self.assertIn("123456789=answer", args)

    def test_distributed_downloads_keep_token_and_directory_archive_options(self):
        self.python_test("DATA(sbr://123456789=answer)")
        graph = self.graph("-t", "--dist", env={"YA_TOKEN": "e2e-placeholder-token", "YA_DIR_OUTPUTS_IN_NODES": "yes"})
        downloads = [node for node in graph["graph"] if "$(B)/sandbox-storage/123456789/resource" in node["outputs"]]
        self.assertEqual(len(downloads), 1)
        self.assertIn("$(B)/sandbox-storage/123456789/dir_res.tar", downloads[0]["outputs"])
        args = [arg for command in downloads[0]["cmds"] for arg in command["cmd_args"]]
        self.assertIn("--for-dist-build", args)
        self.assertEqual(args[args.index("--token") + 1], "e2e-placeholder-token")

    def test_distributed_resource_validation_keeps_the_explicit_token(self):
        self.write("ya.make", "LIBRARY()\nVALIDATE_DATA_RESTART(e2e)\nFROM_SANDBOX(FILE 123456789 OUT_NOAUTO answer.txt)\nRESOURCE(answer.txt /answer)\nEND()\n")
        graph = self.graph("-t", "--dist", env={"YA_TOKEN": "e2e-placeholder-token"})
        checkers = [args[i + 1] for node in graph["graph"] for command in node["cmds"] for args in [command["cmd_args"]] for i, arg in enumerate(args[:-1]) if arg == "--checker"]
        self.assertTrue(checkers)
        self.assertTrue(any("check_resource --verbose" in checker and "--token e2e-placeholder-token" in checker for checker in checkers), checkers)

