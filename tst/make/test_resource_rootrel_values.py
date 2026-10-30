from tst.make.lib import MakeTest, PROJECT


class ResourceRootrelValuesTest(MakeTest):
    def test_inline_rootrel_values_keep_their_source_dependency(self):
        self.write("ya.make", "LIBRARY()\nRESOURCE(- e2e.path=$ENV{E2E_RESOURCE_EXPR})\nEND()\n")
        self.write("value.txt", "42\n")
        expression = '${rootrel;context=TEXT;input=TEXT:"' + PROJECT + '/value.txt"}'
        graph = self.graph(env={"E2E_RESOURCE_EXPR": expression})
        consumers = [node for node in graph["graph"] if f"$(S)/{PROJECT}/value.txt" in node["inputs"]]
        self.assertTrue(consumers)
        args = [arg for node in consumers for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"e2e.path={PROJECT}/value.txt", args)

    def test_inline_rootrel_values_keep_generated_file_producers(self):
        self.write("ya.make", "LIBRARY()\nCOPY_FILE(template.txt generated.txt)\nRESOURCE(- e2e.path=$ENV{E2E_RESOURCE_EXPR})\nEND()\n")
        self.write("template.txt", "42\n")
        expression = '${rootrel;context=TEXT;input=TEXT:"${ARCADIA_BUILD_ROOT}/' + PROJECT + '/generated.txt"}'
        graph = self.graph(env={"E2E_RESOURCE_EXPR": expression})
        copied = self.node(graph, "generated.txt")
        consumers = [node for node in graph["graph"] if f"$(B)/{PROJECT}/generated.txt" in node["inputs"]]
        self.assertTrue(consumers)
        self.assertTrue(any(copied["uid"] in node["deps"] for node in consumers))

    def test_incomplete_inline_rootrel_values_are_rejected(self):
        self.write("ya.make", "LIBRARY()\nRESOURCE(- e2e.path=$ENV{E2E_RESOURCE_EXPR})\nEND()\n")
        expression = '${rootrel;context=TEXT;input=TEXT:"' + PROJECT + '/value.txt'
        self.assertNotEqual(self.make(env={"E2E_RESOURCE_EXPR": expression}).returncode, 0)
