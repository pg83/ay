from tst.make.lib import MakeTest, PROJECT


class SharedResourceObjectsTest(MakeTest):
    def test_native_and_descriptor_consumers_reuse_one_resource_object(self):
        self.write("ya.make", "RECURSE(native schema)\n")
        self.write("native/ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/assets)\nEND()\n")
        self.write("native/main.cpp", "int main() { return 0; }\n")
        self.write("schema/ya.make", f"PROTO_LIBRARY(schema)\nONLY_TAGS(DESC_PROTO)\nSRCS(answer.proto)\nPEERDIR({PROJECT}/assets)\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3"; message Answer {}\n')
        self.write("assets/ya.make", "RESOURCES_LIBRARY()\nRESOURCE(value.txt /answer)\nEND()\n")
        self.write("assets/value.txt", "42\n")
        graph = self.graph()
        resources = [node for node in graph["graph"] if f"$(S)/{PROJECT}/assets/value.txt" in node["inputs"] and any("/objcopy_" in output for output in node["outputs"])]
        self.assertEqual(len(resources), 1)
        output = next(path for path in resources[0]["outputs"] if path.endswith(".o"))
        linked = self.node(graph, "native/answer")
        self.assertIn(output, linked["inputs"])
        self.assertIn(resources[0]["uid"], linked["deps"])
        descriptor = self.node(graph, "schema/answer.proto.desc")
        self.assertIn(f"$(S)/{PROJECT}/schema/answer.proto", descriptor["inputs"])
