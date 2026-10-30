from tst.make.lib import MakeTest, PROJECT


def command_args(node):
    return [arg for command in node["cmds"] for arg in command["cmd_args"]]


class SandboxGraphOptionsTest(MakeTest):
    def test_validation_restart_preserves_the_downloaded_resource_producer(self):
        uids = []
        for extension in ("first", "second"):
            self.write("ya.make", f"LIBRARY()\nVALIDATE_DATA_RESTART({extension})\nFROM_SANDBOX(FILE 123456789 OUT_NOAUTO value.txt)\nRESOURCE(value.txt /value)\nEND()\n")
            uids.append(self.node(self.graph(), "value.txt")["uid"])
        self.assertEqual(*uids)

    def test_python_two_only_tasks_are_rejected_when_forbidden(self):
        self.write("ya.make", "SANDBOX_TASK()\nPY_SRCS(answer.py)\nEND()\n")
        self.write("answer.py", "ANSWER = 42\n")
        self.assertNotEqual(self.make("-DFAIL_PY2=yes").returncode, 0)

    def test_archive_prefix_and_output_includes_reach_the_consumer(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{PROJECT} {PROJECT})\nFROM_SANDBOX(123456789 PREFIX extracted OUT_NOAUTO extracted/value.h OUTPUT_INCLUDES dependency.h)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", '#include "extracted/value.h"\nint answer() { return 42; }\n')
        self.write("dependency.h", "#pragma once\n")
        graph = self.graph()
        extracted = self.node(graph, "extracted/value.h")
        args = command_args(extracted)
        self.assertEqual(args[args.index("--untar-to") + 1], "extracted")
        compiled = self.node(graph, "answer.cpp.o")
        self.assertIn(extracted["uid"], compiled["deps"])
        self.assertIn(f"$(S)/{PROJECT}/dependency.h", compiled["inputs"])

    def test_autoupdated_resource_with_an_explicit_sandbox_prefix(self):
        self.write("ya.make", "LIBRARY()\nFROM_SANDBOX(FILE 123456789 AUTOUPDATED update.py SBR sbr: RENAME RESOURCE OUT_NOAUTO value.txt)\nRESOURCE(value.txt /value)\nEND()\n")
        self.write("update.py", "print(123456789)\n")
        extracted = self.node(self.graph(), "value.txt")
        args = command_args(extracted)
        self.assertEqual(args[args.index("--resource-id") + 1], "123456789")
        self.assertEqual(args[args.index("--rename") + 1], "RESOURCE")
        self.assertIn("--copy-to-dir", args)

    def test_auto_sources_and_manual_outputs_share_one_resource(self):
        self.write("ya.make", "LIBRARY()\nFROM_SANDBOX(123456789 OUT answer.cpp OUT_NOAUTO value.txt)\nRESOURCE(value.txt /value)\nEND()\n")
        graph = self.graph()
        source = self.node(graph, "answer.cpp")
        self.assertIn(f"$(B)/{PROJECT}/value.txt", source["outputs"])
        self.assertIn(source["uid"], self.node(graph, "answer.cpp.o")["deps"])
