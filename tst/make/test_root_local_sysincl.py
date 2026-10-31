from tst.make.lib import MakeTest, PROJECT


class RootLocalSysinclTest(MakeTest):
    def test_root_local_quoted_header_precedes_multi_target_mapping(self):
        self.write_tree("ya.make", "LIBRARY(root)\nSRCS(__ay_root_e2e__.cpp)\nEND()\n")
        self.write_tree("__ay_root_e2e__.cpp", '#include "__ay_root_e2e__.h"\nint answer() { return ANSWER; }\n')
        self.write_tree("__ay_root_e2e__.h", "#define ANSWER 42\n")
        self.write("mapped.h", "#error local header should take precedence\n")
        config = self.root / "build/sysincl/misc.yml"
        self.write_tree("build/sysincl/misc.yml", config.read_text() + f'\n- source_filter: "^__ay_root_e2e__"\n  includes:\n    - __ay_root_e2e__.h:\n      - __ay_root_e2e__.h\n      - {PROJECT}/mapped.h\n')
        self.project_name = "."
        graph = self.graph()
        compiled = [node for node in graph["graph"] if any(output.endswith("/__ay_root_e2e__.cpp.o") for output in node["outputs"])]
        self.assertEqual(len(compiled), 1)
        self.assertTrue(any(path.endswith("/__ay_root_e2e__.h") for path in compiled[0]["inputs"]))
        self.assertNotIn(f"$(S)/{PROJECT}/mapped.h", compiled[0]["inputs"])
