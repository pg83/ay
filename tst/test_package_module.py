import unittest

import lib


def files():
    return {
        "pkg/ya.make": "PACKAGE(bundle)\nFILES(local.txt)\nPEERDIR(data)\nEND()\n",
        "pkg/local.txt": "local\n",
        "data/ya.make": "UNION()\nFILES(asset.txt)\nEND()\n",
        "data/asset.txt": "asset\n",
        "collector/ya.make": "UNION()\nPEERDIR(pkg)\nEND()\n",
        "checks/ya.make": "EXECTEST()\nDEPENDS(pkg)\nRUN(${ARCADIA_BUILD_ROOT}/pkg/local.txt)\nEND()\n",
    }


class PackageModuleTest(unittest.TestCase):
    def test_missing_peer_stays_inside_the_keep_going_module_boundary(self):
        sources = {
            "owner/ya.make": f"PROGRAM()\n{lib.BARE_MODULE}SRCS(main.cpp)\nPEERDIR(absent good)\nEND()\n",
            "owner/main.cpp": "int main_value;\n",
            "good/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}SRCS(good.cpp)\nEND()\n",
            "good/good.cpp": "int good_value;\n",
        }
        graph = lib.make(sources, "owner", "-k")
        compiled = lib.node_by_output(graph, "$(B)/owner/main.cpp.o")
        owner = lib.node_by_output(graph, "$(B)/owner/owner")
        peer = lib.node_by_output(graph, "$(B)/good/libgood.a")
        self.assertIn("$(S)/owner/main.cpp", compiled["inputs"])
        self.assertIn(compiled["uid"], owner["deps"])
        self.assertIn(peer["uid"], owner["deps"])

    def test_package_has_final_and_union_outputs_with_shared_copy_producers(self):
        graph = lib.make(files(), "pkg")
        package = lib.node_by_output(graph, "$(B)/pkg/bundle.final.pkg.fake")
        own_union = lib.node_by_output(graph, "$(B)/pkg/bundle.pkg.fake")
        peer = lib.node_by_output(graph, "$(B)/data/data.pkg.fake")
        self.assertIn(package["uid"], graph["result"])
        self.assertNotIn(own_union["uid"], graph["result"])
        self.assertIn(own_union["uid"], package["deps"])
        self.assertIn(peer["uid"], package["deps"])
        self.assertIn("$(B)/pkg/local.txt", package["inputs"])
        self.assertNotIn("$(B)/data/asset.txt", package["inputs"])
        copy_cmd = next(cmd["cmd_args"] for cmd in package["cmds"] if "$(S)/build/scripts/copy_to_dir.py" in cmd["cmd_args"])
        self.assertIn("$(B)/data/asset.txt", copy_cmd)
        self.assertIn("$(B)/pkg/pkg/local.txt", package["outputs"])
        self.assertIn("$(B)/pkg/data/asset.txt", package["outputs"])
        copies = [node for node in graph["graph"] if node["kv"].get("p") == "CP" and "$(B)/pkg/local.txt" in node["outputs"]]
        self.assertEqual(len(copies), 1)

    def test_ci_group_selects_the_final_package_after_a_union_peer(self):
        sources = files()
        sources["group/ya.make"] = "CI_GROUP()\nPEERDIR(pkg)\nEND()\n"
        for targets in (("group",), ("collector", "group"), ("group", "collector")):
            with self.subTest(targets=targets):
                graph = lib.make(sources, *targets)
                package = lib.node_by_output(graph, "$(B)/pkg/bundle.final.pkg.fake")
                group = lib.node_by_output(graph, "$(B)/group/group.ci.pkg.fake")
                self.assertIn(package["outputs"][0], group["inputs"])
                self.assertIn(package["uid"], group["deps"])

    def test_only_selected_start_targets_become_results(self):
        for tags in ("", "PACKAGE_FINAL", "PACKAGE_UNION"):
            with self.subTest(tags=tags):
                sources = files()
                select = f"ONLY_TAGS({tags})\n" if tags else ""
                sources["pkg/ya.make"] = "PACKAGE(bundle)\n" + select + "FILES(local.txt)\nEND()\n"
                graph = lib.make(sources, "pkg")
                outputs = {p for n in graph["graph"] for p in n["outputs"]}
                if tags == "PACKAGE_UNION":
                    self.assertNotIn("$(B)/pkg/bundle.final.pkg.fake", outputs)
                    self.assertNotIn("$(B)/pkg/bundle.pkg.fake", outputs)
                else:
                    package = lib.node_by_output(graph, "$(B)/pkg/bundle.final.pkg.fake")
                    self.assertIn(package["uid"], graph["result"])
                    if tags == "PACKAGE_FINAL":
                        self.assertNotIn("$(B)/pkg/bundle.pkg.fake", outputs)

    def test_package_variants_share_the_sandbox_producer(self):
        sources = files()
        sources["pkg/ya.make"] = "PACKAGE(bundle)\nFROM_SANDBOX(FILE 123 OUT tool)\nEND()\n"
        graph = lib.make(sources, "checks", "pkg", "-t")
        package = lib.node_by_output(graph, "$(B)/pkg/bundle.final.pkg.fake")
        own_union = lib.node_by_output(graph, "$(B)/pkg/bundle.pkg.fake")
        resources = [node for node in graph["graph"] if node["kv"].get("p") == "SB" and "$(B)/pkg/tool" in node["outputs"]]
        self.assertEqual(len(resources), 1)
        self.assertIn(resources[0]["uid"], own_union["deps"])
        self.assertIn(resources[0]["uid"], package["deps"])
        self.assertIn("$(B)/pkg/tool", package["inputs"])
        self.assertIn("$(B)/pkg/pkg/tool", package["outputs"])

    def test_explicit_test_dependency_builds_the_package_final_output(self):
        for targets in (("checks",), ("checks", "pkg"), ("pkg", "checks"), ("collector", "checks"), ("checks", "collector")):
            with self.subTest(targets=targets):
                graph = lib.make(files(), *targets, "-t")
                package = lib.node_by_output(graph, "$(B)/pkg/bundle.final.pkg.fake")
                suite = lib.node_by_output(graph, "$(B)/checks/test-results/exectest/meta.json")
                self.assertIn(package["uid"], suite["deps"])
                self.assertEqual(len([node for node in graph["graph"] if "$(B)/pkg/bundle.final.pkg.fake" in node["outputs"]]), 1)


if __name__ == "__main__":
    unittest.main(verbosity=2)
