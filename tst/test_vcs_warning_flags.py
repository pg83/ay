import unittest

import lib
from test_go_package import go_module, go_world


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
KINDS = ("program", "unittest", "dll", "dynamic", "go")
POLICIES = {
    "plain": "",
    "shadow": "NO_WSHADOW()\n",
    "quiet_last": "NO_WSHADOW()\nNO_COMPILER_WARNINGS()\n",
    "shadow_last": "NO_COMPILER_WARNINGS()\nNO_WSHADOW()\n",
}


def fixture(kinds=KINDS, policies=POLICIES):
    files = go_world()
    files["build/platform/local_so/ya.make"] = f"LIBRARY()\n{BARE}END()\n"
    files["library/cpp/testing/unittest_main/ya.make"] = f"LIBRARY()\n{BARE}END()\n"
    lib.tool_program(files, "tools/fix_elf", "fix_elf")
    roots = []
    for kind in kinds:
        for policy in policies:
            directory = f"orchard/{kind}_{policy}"
            flags = POLICIES[policy]
            roots.append(directory)
            if kind == "go":
                go_module(files, directory, module="GO_PROGRAM", macros=BARE + flags)
                continue
            files[directory + "/a.c"] = "int c;\n"
            files[directory + "/b.cpp"] = "int cpp;\n"
            body = f"{BARE}SRCS(a.c b.cpp)\n{flags}"
            if kind == "unittest":
                peer = directory + "/peer"
                files[peer + "/ya.make"] = f"LIBRARY()\n{BARE}END()\n"
                header = f"UNITTEST_FOR({peer})"
            elif kind == "dynamic":
                peer = directory + "/peer"
                files[peer + "/ya.make"] = f"LIBRARY()\n{BARE}SRCS(peer.cpp)\nEND()\n"
                files[peer + "/peer.cpp"] = "int peer;\n"
                header = "DYNAMIC_LIBRARY(sprout)"
                body = f"{BARE}{flags}EXPORTS_SCRIPT(sprout.exports)\nDYNAMIC_LIBRARY_FROM({peer})\n"
                files[directory + "/sprout.exports"] = "{};\n"
            else:
                header = "DLL(sprout)" if kind == "dll" else "PROGRAM(sprout)"
            if kind == "program" and policy == "plain":
                body += "PEERDIR(peer)\n"
                files["peer/ya.make"] = f"LIBRARY()\n{BARE}NO_WSHADOW()\nSRCS(peer.c)\nEND()\n"
                files["peer/peer.c"] = "int peer;\n"
            files[directory + "/ya.make"] = f"{header}\n{body}END()\n"
    return files, roots


def vcs_compile(graph, directory):
    return next(
        command["cmd_args"]
        for node in graph["graph"]
        for command in node["cmds"]
        if any(arg.endswith(f"/{directory}/__vcs_version__.c") for arg in command["cmd_args"])
        and "-c" in command["cmd_args"]
    )


class VcsWarningFlagsTest(unittest.TestCase):
    def test_no_shadow_reaches_embedded_vcs_c_for_all_linking_consumers(self):
        files, roots = fixture(policies=("shadow",))
        graph = lib.make(files, *roots)
        for directory in roots:
            with self.subTest(directory=directory):
                args = vcs_compile(graph, directory)
                self.assertEqual(args.count("-Wno-shadow"), 1)
                self.assertLess(args.index("-Wno-unknown-warning-option"), args.index("-Wno-shadow"))
                self.assertLess(args.index("-Wno-shadow"), args.index("-DARCADIA_ROOT=$(S)"))

    def test_no_compiler_warnings_replaces_shadow_bundle_in_both_orders(self):
        files, roots = fixture(policies=("quiet_last", "shadow_last"))
        graph = lib.make(files, *roots)
        for directory in roots:
            with self.subTest(directory=directory):
                args = vcs_compile(graph, directory)
                self.assertIn("-Wno-everything", args)
                self.assertNotIn("-Wno-shadow", args)
                self.assertNotIn("-Wall", args)

    def test_peer_warning_policy_does_not_leak_to_owner_or_sibling(self):
        files, roots = fixture(kinds=("program", "dll"), policies=("plain", "shadow"))
        graph = lib.make(files, *roots)
        for directory in roots:
            with self.subTest(directory=directory):
                args = vcs_compile(graph, directory)
                self.assertEqual("-Wno-shadow" in args, directory.endswith("_shadow"))
        self.assertIn("-Wno-shadow", lib.node_by_output(graph, "$(B)/peer/peer.c.o")["cmds"][0]["cmd_args"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
