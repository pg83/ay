import json
import unittest

from test_devtools_dump import DumpToolTest, compact, raw_node


class DumpDependencyAliasesTest(DumpToolTest):
    def normalize(self, nodes, *flags):
        graph = self.write("aliases.json", compact({"graph": nodes, "result": ["root"]}))
        result = self.ok("dev", "dump", "normalize", "--in", graph, *flags)
        return sorted(map(compact, map(json.loads, result.stdout.splitlines())))

    def test_coalesced_producers_keep_one_dependency_and_stable_ancestor_uids(self):
        leaf = raw_node("leaf", "CP", ["$(B)/shared/payload"], inputs=["$(S)/data/payload"])
        producer = raw_node("producer", "PR", ["$(B)/shared/prepared"], deps=["leaf"])
        root = raw_node("root", "TS", ["$(B)/checks/meta.json"], deps=["producer", "external-a", "external-b"])
        canonical = [leaf, producer, root]
        aliases = [
            leaf,
            dict(leaf, uid="leaf-alias", inputs=["$(SOURCE_ROOT)/data/payload"]),
            producer,
            dict(producer, uid="producer-alias", deps=["leaf-alias", "leaf"]),
            dict(root, deps=["producer-alias", "external-b", "producer", "external-a", "producer-alias"]),
        ]
        expected = self.normalize(canonical)
        for graph in (aliases, list(reversed(aliases))):
            for flags in ((), ("--streaming",), ("--ref-graph",), ("--ref-graph", "--streaming")):
                with self.subTest(flags=flags, reversed=graph is not aliases):
                    actual = self.normalize(graph, *flags)
                    self.assertEqual(actual, expected)
                    nodes = [json.loads(line) for line in actual]
                    self.assertEqual(len(nodes), 3)
                    parent = next(node for node in nodes if node["kv"]["p"] == "TS")
                    self.assertIn("external-a", parent["deps"])
                    self.assertIn("external-b", parent["deps"])
                    self.assertEqual(len(parent["deps"]), 3)

    def test_distinct_producer_contents_and_inputs_remain_distinct(self):
        producers = [
            raw_node("first", "PR", ["$(B)/shared/prepared"], inputs=["$(S)/data/first"]),
            raw_node("second", "PR", ["$(B)/shared/prepared"], inputs=["$(S)/data/second"]),
        ]
        root = raw_node("root", "TS", ["$(B)/checks/meta.json"], deps=["first", "second"])
        for flags in ((), ("--streaming",), ("--ref-graph",)):
            with self.subTest(flags=flags):
                nodes = [json.loads(line) for line in self.normalize([root, *producers], *flags)]
                self.assertEqual(len(nodes), 3)
                parent = next(node for node in nodes if node["kv"]["p"] == "TS")
                children = [node for node in nodes if node["kv"]["p"] == "PR"]
                self.assertEqual(parent["deps"], sorted(node["uid"] for node in children))
                self.assertEqual(sorted(node["inputs"] for node in children), [["$(S)/data/first"], ["$(S)/data/second"]])


if __name__ == "__main__":
    unittest.main()
