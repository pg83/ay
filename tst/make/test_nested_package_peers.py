from tst.make.lib import MakeTest, PROJECT


class NestedPackagePeersTest(MakeTest):
    def test_nested_packages_keep_file_contents_in_the_parent(self):
        self.write("ya.make", f"PACKAGE(bundle)\nPEERDIR({PROJECT}/child)\nEND()\n")
        self.write("child/ya.make", "PACKAGE(child)\nFILES(value.txt)\nEND()\n")
        self.write("child/value.txt", "42\n")
        graph = self.graph()
        packaged = self.node(graph, "bundle.final.pkg.fake")
        child = self.node(graph, "child/child.pkg.fake")
        self.assertIn(f"$(B)/{PROJECT}/child/child.pkg.fake", packaged["inputs"])
        self.assertIn(child["uid"], packaged["deps"])
        self.assertIn(f"$(B)/{PROJECT}/child/value.txt", child["inputs"])
        copies = [node for node in graph["graph"] if f"$(S)/{PROJECT}/child/value.txt" in node["inputs"] and f"$(B)/{PROJECT}/child/value.txt" in node["outputs"]]
        self.assertEqual(len(copies), 1)
        self.assertIn(copies[0]["uid"], child["deps"])

    def test_ci_groups_deduplicate_diamond_collection_dependencies(self):
        self.write("ya.make", f"CI_GROUP(bundle)\nPEERDIR({PROJECT}/hub)\nEND()\n")
        self.write("hub/ya.make", f"UNION(hub)\nPEERDIR({PROJECT}/left {PROJECT}/right)\nEND()\n")
        for side in ("left", "right"):
            self.write(f"{side}/ya.make", f"UNION({side})\nPEERDIR({PROJECT}/leaf)\nEND()\n")
        self.write("leaf/ya.make", "UNION(leaf)\nFILES(value.txt)\nEND()\n")
        self.write("leaf/value.txt", "42\n")
        graph = self.graph()
        packaged = self.node(graph, "bundle.ci.pkg.fake")
        leaf = self.node(graph, "leaf/leaf.pkg.fake")
        self.assertEqual(leaf["inputs"].count(f"$(B)/{PROJECT}/leaf/value.txt"), 1)
        nodes = {node["uid"]: node for node in graph["graph"]}
        pending = [packaged["uid"]]
        visited = set()
        while pending:
            uid = pending.pop()
            if uid in visited:
                continue
            visited.add(uid)
            pending.extend(nodes.get(uid, {}).get("deps", []))
        self.assertIn(leaf["uid"], visited)
