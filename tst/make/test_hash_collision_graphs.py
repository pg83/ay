from tst.make.lib import MakeTest, PROJECT


class HashCollisionGraphsTest(MakeTest):
    def setUp(self):
        super().setUp()
        self.write("ya.make", f"PROGRAM(answer)\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{PROJECT})\nCFLAGS(-DE2E_FIRST=1 -DE2E_SECOND=2)\nCOPY_FILE(template.h generated.h)\nSRCS(main.cpp other.cpp)\nRESOURCE(value.txt /e2e/value)\nEND()\n")
        self.write("template.h", "#define VALUE 42\n")
        self.write("main.cpp", '#include "generated.h"\nint main() { return VALUE; }\n')
        self.write("other.cpp", '#include "generated.h"\nint other() { return VALUE; }\n')
        self.write("value.txt", "42\n")

    def assert_dependencies(self, chaos):
        graph = self.graph(env={"AY_CHAOS": chaos})
        generated = self.node(graph, "generated.h")
        for source in ("main.cpp", "other.cpp"):
            compiled = self.node(graph, source + ".o")
            self.assertIn(f"$(S)/{PROJECT}/{source}", compiled["inputs"])
            self.assertIn(f"$(B)/{PROJECT}/generated.h", compiled["inputs"])
            self.assertIn(generated["uid"], compiled["deps"])
            args = [arg for command in compiled["cmds"] for arg in command["cmd_args"]]
            for flag in ("-DE2E_FIRST=1", "-DE2E_SECOND=2"):
                self.assertIn(flag, args)
        self.assertIn(self.node(graph, "answer")["uid"], graph["result"])
        self.assertTrue(any(f"$(S)/{PROJECT}/value.txt" in node["inputs"] for node in graph["graph"]))

    def test_intern_hash_collisions_preserve_source_and_flag_identity(self):
        self.assert_dependencies("xxh-intern-hi=0")

    def test_unshuffled_string_ids_preserve_generated_dependencies(self):
        self.assert_dependencies("intern-shuffle=0")

    def test_primary_slice_hash_collisions_preserve_shared_command_chunks(self):
        self.assert_dependencies("xxh-slice-hi=0")

    def test_zero_secondary_slice_hashes_preserve_shared_command_chunks(self):
        self.assert_dependencies("xxh-slice-lo=0")
