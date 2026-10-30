import json

from tst.make.lib import MakeTest


class DirectoryTestOutputsTest(MakeTest):
    def test_split_tests_preserve_directory_outputs_without_archiving_them(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nFORK_SUBTESTS()\nSPLIT_FACTOR(2)\nEND()\n")
        self.write("test_answer.py", "def test_first(): assert True\ndef test_second(): assert True\n")
        result = self.make("-t", sandboxing=False, env={"YA_DIR_OUTPUTS": "yes", "YA_DIR_OUTPUTS_IN_NODES": "yes"})
        self.assertEqual(result.returncode, 0, result.stderr)
        graph = json.loads(result.stdout)
        commands = [command["cmd_args"] for node in graph["graph"] for command in node["cmds"]]
        runs = [args for args in commands if "run_test" in args and args[args.index("--test-type") + 1] == "py3test"]
        self.assertEqual(len(runs), 2)
        for args in runs:
            self.assertIn("--dir-outputs", args)
            self.assertNotIn("--tar", args)
        merges = [args for args in commands if "--skip-dir" in args]
        self.assertTrue(merges)
        self.assertTrue(any(args[args.index("--skip-dir") + 1] == "testing_out_stuff" for args in merges))
