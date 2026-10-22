"""Module-local USER_CFLAGS appends also feed compiler command arguments."""
import unittest

import lib
from test_codegen_enum import enum_files
from test_proto_cpp import proto_files


# Pinned quality/generative_answer/linters.make.inc, with renamed fixture roots.
PARENTHESES = [
    "-Wparentheses", "-Wno-bitwise-conditional-parentheses",
    "-Wno-bitwise-op-parentheses", "-Wno-dangling-else",
    "-Wno-logical-not-parentheses", "-Wno-logical-op-parentheses",
    "-Wno-overloaded-shift-op-parentheses", "-Wno-parentheses-equality",
    "-Wno-shift-op-parentheses",
]
POLICY = (
    "IF(MODULE_LANG == CPP AND NOT CPP_PROTO)\n"
    "SET_APPEND(USER_CFLAGS " + " ".join(PARENTHESES) + ")\nENDIF()\n"
)


def module(body, kind="LIBRARY"):
    return f"{kind}()\n{lib.BARE_MODULE}{body}END()\n"


def args(graph, output):
    return lib.node_by_output(graph, output)["cmds"][0]["cmd_args"]


class UserCflagsAppendTest(unittest.TestCase):
    def test_autoincluded_policy_reaches_generated_and_c_compiles_and_vcs_link_command(self):
        files = enum_files({
            "build/internal/conf/autoincludes.json": '["orchard"]',
            "orchard/linters.make.inc": POLICY,
            "orchard/core/ya.make": module(
                "GENERATE_ENUM_SERIALIZATION(states.h)\nSRCS(plain.c)\n"
            ),
            "orchard/core/states.h": "enum class EState {Ready};\n",
            "orchard/core/plain.c": "int plain;\n",
            "orchard/driver/ya.make": module("PEERDIR(orchard/core)\nSRCS(main.cpp)\n", "PROGRAM"),
            "orchard/driver/main.cpp": "int main(){return 0;}\n",
        })
        graph = lib.make(files, "orchard/driver")
        for output in ("orchard/core/states.h_serialized.cpp.o", "orchard/core/plain.c.o",
                       "orchard/driver/main.cpp.o"):
            command = args(graph, "$(B)/" + output)
            self.assertEqual([flag for flag in command if flag in PARENTHESES], PARENTHESES)
        producer = lib.node_by_output(graph, "$(B)/orchard/core/states.h_serialized.cpp")
        compiled = lib.node_by_output(graph, "$(B)/orchard/core/states.h_serialized.cpp.o")
        self.assertIn("$(S)/orchard/core/states.h", producer["inputs"])
        self.assertIn(producer["uid"], compiled["deps"])
        self.assertIn(producer["outputs"][0], compiled["inputs"])
        self.assertIn("$(S)/orchard/core/states.h", compiled["inputs"])
        archive = lib.node_by_output(graph, "$(B)/orchard/core/liborchard-core.a")
        self.assertIn(compiled["uid"], archive["deps"])
        linked = lib.node_by_output(graph, "$(B)/orchard/driver/driver")
        self.assertIn(archive["uid"], linked["deps"])
        self.assertIn(linked["uid"], graph["result"])
        [vcs] = [command["cmd_args"] for command in linked["cmds"]
                 if "$(B)/orchard/driver/__vcs_version__.c" in command["cmd_args"]
                 and "-c" in command["cmd_args"]]
        self.assertEqual([flag for flag in vcs if flag in PARENTHESES], PARENTHESES)

    def test_append_order_duplicates_and_private_scope_are_preserved(self):
        files = {
            "build/conf/autoincludes.json": '["orchard"]',
            "orchard/linters.make.inc": "SET_APPEND(USER_CFLAGS -DPOLICY -DPOLICY)\n",
            "orchard/leaf/ya.make": module(
                "CFLAGS(-DBEFORE GLOBAL -DEXPORTED)\n"
                "SET(LOCAL_OPTIONS -DMIDDLE -DSECOND)\n"
                "SET_APPEND(USER_CFLAGS ${LOCAL_OPTIONS})\nCFLAGS(-DAFTER)\n"
                "SET_APPEND(SFLAGS -DASSEMBLER_CONTROL)\nSRCS(leaf.cpp asm.S)\n"
            ),
            "orchard/leaf/leaf.cpp": "int leaf;\n",
            "orchard/leaf/asm.S": "",
            "outside/ya.make": module("PEERDIR(orchard/leaf)\nSRCS(main.cpp)\n", "PROGRAM"),
            "outside/main.cpp": "int main(){return 0;}\n",
        }
        graph = lib.make(files, "outside", env={"CFLAGS": "-DBASE"})
        own = [flag for flag in args(graph, "$(B)/orchard/leaf/leaf.cpp.o")
               if flag in {"-DBASE", "-DBEFORE", "-DMIDDLE", "-DSECOND", "-DAFTER", "-DPOLICY", "-DEXPORTED"}]
        self.assertEqual(own, ["-DBASE", "-DBEFORE", "-DMIDDLE", "-DSECOND", "-DAFTER",
                               "-DPOLICY", "-DPOLICY", "-DEXPORTED"])
        consumer = args(graph, "$(B)/outside/main.cpp.o")
        self.assertEqual([flag for flag in consumer if flag in set(own)], ["-DBASE", "-DEXPORTED"])
        self.assertNotIn("-DASSEMBLER_CONTROL", args(graph, "$(B)/orchard/leaf/leaf.cpp.o"))
        self.assertIn("-DASSEMBLER_CONTROL", args(graph, "$(B)/orchard/leaf/asm.S.o"))

    def test_proto_and_nested_policy_contexts_do_not_inherit_the_outer_policy(self):
        files = proto_files()
        files.update({
            "build/conf/autoincludes.json": '["orchard", "orchard/separate"]',
            "orchard/linters.make.inc": POLICY,
            "orchard/separate/linters.make.inc": "SET_APPEND(USER_CFLAGS -DNESTED_POLICY)\n",
            "orchard/wire/ya.make": module("ONLY_TAGS(CPP_PROTO)\nSRCS(message.proto)\n", "PROTO_LIBRARY"),
            "orchard/wire/message.proto": 'syntax = "proto3";\nmessage Wire {}\n',
            "orchard/separate/lib/ya.make": module("SRCS(leaf.cpp)\n"),
            "orchard/separate/lib/leaf.cpp": "int leaf;\n",
        })
        graph = lib.make(files, "orchard/wire", "orchard/separate/lib")
        proto = args(graph, "$(B)/orchard/wire/message.pb.cc.o")
        nested = args(graph, "$(B)/orchard/separate/lib/leaf.cpp.o")
        self.assertFalse(any(flag in proto for flag in PARENTHESES))
        self.assertFalse(any(flag in nested for flag in PARENTHESES))
        self.assertEqual(nested.count("-DNESTED_POLICY"), 1)

    def test_same_policy_is_applied_in_separate_host_and_target_environments(self):
        files = {
            "build/conf/autoincludes.json": '["orchard"]',
            "orchard/linters.make.inc": (
                "IF(ARCH_X86_64)\nSET_APPEND(USER_CFLAGS -DHOST_POLICY)\n"
                "ELSE()\nSET_APPEND(USER_CFLAGS -DTARGET_POLICY)\nENDIF()\n"
            ),
            "orchard/app/ya.make": module("RUN_PROGRAM(orchard/tool OUT generated.cpp)\n"),
        }
        lib.tool_program(files, "orchard/tool", "tool")
        graph = lib.make(files, "orchard/app")
        tool = args(graph, "$(B)/orchard/tool/main.cpp.pic.o")
        target = args(graph, "$(B)/orchard/app/generated.cpp.o")
        self.assertIn("-DHOST_POLICY", tool)
        self.assertNotIn("-DTARGET_POLICY", tool)
        self.assertIn("-DTARGET_POLICY", target)
        self.assertNotIn("-DHOST_POLICY", target)
        producer = lib.node_by_output(graph, "$(B)/orchard/app/generated.cpp")
        compiled = lib.node_by_output(graph, "$(B)/orchard/app/generated.cpp.o")
        self.assertIn(producer["uid"], compiled["deps"])
        self.assertIn(producer["outputs"][0], compiled["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
