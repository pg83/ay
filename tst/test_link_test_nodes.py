import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def library(body=""):
    return f"LIBRARY()\n{NO_PLATFORM}{body}END()\n"


def make_unsandboxed(files, target, *args):
    with tempfile.TemporaryDirectory(prefix="ay-link-test-nodes-") as directory:
        root = Path(directory)
        (root / ".arcadia.root").touch()
        (root / "ya.conf").write_text(
            '[flags]\nOPENSOURCE = "yes"\n\n[host_platform_flags]\nOPENSOURCE = "yes"\n'
        )
        for relative, content in files.items():
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        env = {
            key: value
            for key, value in os.environ.items()
            if key not in lib.TOOLCHAIN_ENV_VARS
        }
        result = lib.run(
            "make", "-j0", "-G",
            "--source-root", root,
            "--target-platform", "default-linux-aarch64",
            "--host-platform", "default-linux-x86_64",
            *args, target,
            env=env,
        )
        return json.loads(result.stdout)


def fixture():
    return {
        "lib/ya.make": library("SRCS(a.cpp)\n"),
        "lib/a.cpp": "int a(){return 1;}\n",
        "library/cpp/testing/unittest_main/ya.make": library("SRCS(m.cpp)\n"),
        "library/cpp/testing/unittest_main/m.cpp": "int main(){return 0;}\n",
        "res/ya.make": (
            "RESOURCES_LIBRARY()\n"
            "DECLARE_EXTERNAL_RESOURCE(ZTOOL sbr:22 ATOOL sbr:11)\n"
            "END()\n"
        ),
        "lib/ut/ya.make": (
            f"UNITTEST_FOR(lib)\n{NO_PLATFORM}"
            "SRCS(a_ut.cpp GLOBAL g.cpp x.c h.h sub/y.cc)\n"
            "SRC_C_AVX(v.cpp)\n"
            "PEERDIR(res)\n"
            "END()\n"
        ),
        "lib/a_ut.cpp": "int main(){return 0;}\n",
        "lib/g.cpp": "int g;\n",
        "lib/x.c": "int x;\n",
        "lib/h.h": "\n",
        "lib/v.cpp": "int v;\n",
        "lib/sub/y.cc": "int y;\n",
    }


def result_outputs(graph):
    by_uid = {node["uid"]: node for node in graph["graph"]}
    return [by_uid[ref]["outputs"][0] for ref in graph["result"]]


def suite_outputs(suite):
    base = f"$(B)/lib/ut/test-results/{suite}"
    return [
        f"{base}/meta.json",
        f"{base}/ytest.report.trace",
        f"{base}/run_test.log",
        f"{base}/testing_out_stuff.tar.zstd",
    ]


class TestNodesTest(unittest.TestCase):
    def test_unittest_for_emits_context_run_and_style_nodes(self):
        graph = lib.make(fixture(), "lib/ut", "-t", "-r")
        self.assertEqual(
            result_outputs(graph),
            [
                "$(B)/lib/ut/lib-ut",
                suite_outputs("unittest")[0],
                suite_outputs("clang_format")[0],
            ],
        )

        context = lib.node_by_output(graph, "$(B)/common_test.context")
        self.assertEqual(context["kv"], {"p": "CP", "pc": "light-blue"})
        self.assertEqual(
            context["cmds"][0]["cmd_args"][:3],
            [
                "$(YMAKE_PYTHON3)/bin/python3",
                "$(S)/build/scripts/append_file.py",
                "$(B)/common_test.context",
            ],
        )
        self.assertIn('  "build_type": "release",', context["cmds"][0]["cmd_args"])
        self.assertEqual(context["inputs"], ["$(S)/build/scripts/append_file.py"])
        self.assertEqual(
            context["requirements"], {"network": "restricted"}
        )

        binary = lib.node_by_output(graph, "$(B)/lib/ut/lib-ut")
        run = lib.node_by_output(graph, suite_outputs("unittest")[0])
        self.assertEqual(run["outputs"], suite_outputs("unittest"))
        self.assertEqual(run["deps"], [binary["uid"], context["uid"]])
        self.assertEqual(
            run["kv"],
            {
                "disable_cache": "yes",
                "p": "TS",
                "path": "lib/ut/unittest",
                "pc": "yellow",
                "run_test_node": True,
                "show_out": True,
                "special_runner": "",
            },
        )
        self.assertEqual(
            run["requirements"],
            {"cpu": 1, "network": "restricted", "ram": 8, "ram_disk": 0},
        )
        self.assertEqual(run["env"]["TEST_NAME"], "unittest")
        self.assertEqual(run["env"]["YA_TEST_RUNNER"], "1")
        self.assertEqual(run["cmds"][0]["cwd"], "$(B)")
        self.assertEqual(run["inputs"], ["$(S)/lib/ut"])
        text = " ".join(run["cmds"][0]["cmd_args"])
        self.assertIn(
            "--target-platform-descriptor "
            "default-linux-aarch64-release-FAKEID=sandboxing-SANDBOXING=yes",
            text,
        )
        self.assertIn(
            "--global-resource ATOOL_RESOURCE_GLOBAL::$(B)/resources/ATOOL "
            "--global-resource ZTOOL_RESOURCE_GLOBAL::$(B)/resources/ZTOOL "
            "--ram-limit-gb 8",
            text,
        )
        self.assertIn("run_ut --binary $(B)/lib/ut/lib-ut", text)

        style = lib.node_by_output(graph, suite_outputs("clang_format")[0])
        self.assertEqual(style["deps"], [context["uid"]])
        self.assertNotIn("disable_cache", style["kv"])
        self.assertEqual(style["env"]["TEST_NAME"], "clang_format")
        self.assertEqual(
            style["inputs"],
            [
                "$(S)/build/config/tests/cpp_style/.clang-format",
                "$(S)/build/scripts/c_templates/svn_interface.c",
                "$(S)/tools/cpp_style_checker/wrapper.py",
                "$(S)/lib/ut",
                "$(S)/lib/a_ut.cpp",
                "$(S)/lib/x.c",
                "$(S)/lib/sub/y.cc",
            ],
        )
        style_text = " ".join(style["cmds"][0]["cmd_args"])
        self.assertIn(
            "--test-related-path $(S)/lib/a_ut.cpp "
            "--test-related-path $(S)/lib/x.c "
            "--test-related-path $(S)/lib/sub/y.cc",
            style_text,
        )
        self.assertTrue(
            style_text.endswith(
                "$(S)/build/scripts/c_templates/svn_interface.c "
                "$(S)/lib/a_ut.cpp $(S)/lib/x.c $(S)/lib/sub/y.cc "
                "--ya-end-command-file"
            )
        )

    def test_unsandboxed_debug_descriptor(self):
        graph = make_unsandboxed(fixture(), "lib/ut", "-t")
        context = lib.node_by_output(graph, "$(B)/common_test.context")
        self.assertIn('  "build_type": "debug",', context["cmds"][0]["cmd_args"])
        run = lib.node_by_output(graph, suite_outputs("unittest")[0])
        args = run["cmds"][0]["cmd_args"]
        descriptor = args[args.index("--target-platform-descriptor") + 1]
        self.assertEqual(descriptor, "default-linux-aarch64-debug")

    def test_test_nodes_require_flag_and_unittest_root(self):
        files = fixture()
        graph = lib.make(files, "lib/ut")
        self.assertEqual(result_outputs(graph), ["$(B)/lib/ut/lib-ut"])
        files["prog/ya.make"] = (
            f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(lib)\nEND()\n"
        )
        files["prog/m.cpp"] = "int main(){return 0;}\n"
        graph = lib.make(files, "prog", "-t")
        self.assertEqual(result_outputs(graph), ["$(B)/prog/prog"])
        kinds = {node["kv"]["p"] for node in graph["graph"]}
        self.assertNotIn("TS", kinds)


if __name__ == "__main__":
    unittest.main(verbosity=2)
