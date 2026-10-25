import unittest

from tst import lib
from tst.make.test_link_test_nodes import fixture
from tst.make.test_native_swig import perl_tree


class CliContractsTest(unittest.TestCase):
    def test_implicit_perl_binding_names(self):
        for module, name in (("DLL()", "bindings-perl"), ("PROGRAM()", "perl"),
                             ("LIBRARY()", "bindings-perl")):
            with self.subTest(module=module):
                graph = lib.make(perl_tree(module), "bindings/perl")
                generated = lib.node_by_output(graph, "$(B)/bindings/perl/__/api_wrap.swg.cpp")
                self.assertIn(f"$(B)/bindings/perl/{name}.pm", generated["outputs"])
                args = generated["cmds"][0]["cmd_args"]
                self.assertEqual(args[args.index("-module") + 1], name)

    def test_inherited_test_environment(self):
        files = fixture()
        files["lib/ut/ya.make"] = files["lib/ut/ya.make"].replace(
            "END()", "ENV(AY_TEST_CONTRACT_VALUE)\nENV(AY_TEST_CONTRACT_MISSING_592)\nFORK_TESTS()\nEND()")
        graph = lib.make(files, "lib/ut", "-t", env={
            "AY_TEST_CONTRACT_VALUE": "forwarded", "YA_MANDATORY_ENV_VARS": "YA_TEST_TASK_HINT",
            "YA_TEST_TASK_HINT": "present", "YA_TEST_DISABLE_FLAKE8_MIGRATIONS": "no",
        })
        run = lib.node_by_output(graph, "$(B)/lib/ut/test-results/unittest/chunk0/meta.json")
        args = run["cmds"][-1]["cmd_args"]
        self.assertIn("AY_TEST_CONTRACT_VALUE=forwarded", args)
        self.assertNotIn("AY_TEST_CONTRACT_MISSING_592", args)
        self.assertIn("YA_TEST_TASK_HINT", run["env"]["YA_MANDATORY_ENV_VARS"])
        self.assertNotIn("DISABLE_FLAKE8_MIGRATIONS=yes", args)
        lib.node_by_output(graph, "$(B)/lib/ut/test-results/unittest/chunk9/meta.json")

    def test_dist_split_and_data_downloads(self):
        files = fixture()
        files["lib/ut/ya.make"] = files["lib/ut/ya.make"].replace(
            "END()", "SPLIT_FACTOR(2)\nDATA(sbr://123=fixture)\nEND()")
        graph = lib.make(files, "lib/ut", "-t", "--dist")
        combined = lib.node_by_output(graph, "$(B)/lib/ut/test-results/unittest/meta.json")
        args = combined["cmds"][0]["cmd_args"]
        self.assertIn("--truncate", args)
        download = lib.node_by_output(graph, "$(B)/sandbox-storage/123/resource")
        self.assertIn("--for-dist-build", download["cmds"][0]["cmd_args"])

    def test_named_exec_and_traceback(self):
        files = {"app/ya.make": "EXECTEST(named)\nRUN(check)\nEND()\n"}
        graph = lib.make(files, "app", "-t", "--test-traceback", "short")
        lib.node_by_output(graph, "$(B)/app/named.pkg.fake")
        run = lib.node_by_output(graph, "$(B)/app/test-results/exectest/meta.json")
        args = run["cmds"][-1]["cmd_args"]
        self.assertEqual(args[args.index("--tb") + 1], "short")
        failed = lib.make_process({"app/ya.make": "EXECTEST()\nEND()\n"}, "app")
        self.assertNotEqual(failed.returncode, 0)
        self.assertIn("must have at least one RUN", failed.stderr)

    def test_docker_tag_first_with_system_python(self):
        files = {"app/ya.make": "EXECTEST()\nRUN(check)\n"
                 "DOCKER_IMAGE(fixture=docker://registry.test/image@sha256:1234567890abcdef)\nEND()\n"}
        graph = lib.make(files, "app", "-t", "-DUSE_ARCADIA_PYTHON=no")
        image = lib.only_node_by_kind(graph, "IM")
        args = image["cmds"][0]["cmd_args"]
        self.assertEqual(args[0], "python3")
        self.assertEqual(args[args.index("--tag") + 1], "fixture")


if __name__ == "__main__":
    unittest.main(verbosity=2)
