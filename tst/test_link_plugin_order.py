import unittest

import lib
from test_go_package import go_world


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def project(go, mixed):
    files = go_world() if go else {}
    for name, peers in (("common", ""), ("left", "common"), ("right", "common")):
        body = "PEERDIR(common)\n" if peers else ""
        body += "LD_PLUGIN(generated.py)\nSRCS(local.cpp"
        body += " GLOBAL raw.pyplugin" if mixed else ""
        body += ")\n"
        files[f"{name}/ya.make"] = f"LIBRARY()\n{BARE}{body}END()\n"
        files[f"{name}/local.cpp"] = f"int {name};\n"
        files[f"{name}/generated.py"] = "pass\n"
        files[f"{name}/raw.pyplugin"] = "pass\n"
    module, source, content = ("GO_PROGRAM", "main.go", "package main\nfunc main() {}\n") if go else ("PROGRAM", "main.cpp", "int main() {}\n")
    raw_first = " GLOBAL first.pyplugin" if mixed else ""
    raw_second = "SRCS(GLOBAL second.pyplugin)\n" if mixed else ""
    files["app/ya.make"] = (
        f"{module}()\n{BARE}LD_PLUGIN(early.py)\nSRCS({source}{raw_first})\n"
        f"LD_PLUGIN(late.py)\n{raw_second}PEERDIR(right left)\nEND()\n"
    )
    files["app/" + source] = content
    for name in ("early.py", "late.py", "first.pyplugin", "second.pyplugin"):
        files["app/" + name] = "pass\n"
    return files


class LinkPluginOrderTest(unittest.TestCase):
    def test_postfix_peers_then_own_sources_and_deferred_outputs(self):
        for go in (False, True):
            for mixed in (False, True):
                with self.subTest(go=go, mixed=mixed):
                    graph = lib.make(project(go, mixed), "app")
                    link = lib.node_by_output(graph, "$(B)/app/app")
                    marker, end = ("++ld_plugins", "++vcs") if go else ("--start-plugins", "--end-plugins")
                    args = next(cmd["cmd_args"] for cmd in link["cmds"] if marker in cmd["cmd_args"])
                    actual = args[args.index(marker) + 1:args.index(end)]
                    expected = []
                    for name in ("common", "right", "left"):
                        if mixed:
                            expected.append(f"$(S)/{name}/raw.pyplugin")
                        expected.append(f"$(B)/{name}/generated.py.pyplugin")
                    if mixed:
                        expected.extend(("$(S)/app/first.pyplugin", "$(S)/app/second.pyplugin"))
                    expected.extend(("$(B)/app/early.py.pyplugin", "$(B)/app/late.py.pyplugin"))
                    self.assertEqual(actual, expected)
                    for plugin in expected:
                        self.assertIn(plugin, link["inputs"])
                        self.assertEqual(link["inputs"].count(plugin), 1)
                        if plugin.startswith("$(B)"):
                            producer = lib.node_by_output(graph, plugin)
                            self.assertIn(producer["uid"], link["deps"])
                            self.assertEqual(link["deps"].count(producer["uid"]), 1)


if __name__ == "__main__":
    unittest.main(verbosity=2)
