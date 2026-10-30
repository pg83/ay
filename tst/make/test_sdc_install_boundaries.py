from tst.make.lib import MakeTest


class SdcInstallBoundariesTest(MakeTest):
    def test_target_installations_require_a_target_path(self):
        self.write("ya.make", "SDC_UNION(bundle)\nSDC_INSTALL(LIB TARGET)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_installations_require_a_kind_before_a_file(self):
        self.write("ya.make", "SDC_UNION(bundle)\nSDC_INSTALL(value.txt)\nEND()\n")
        self.write("value.txt", "42\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_program_starters_are_generated_in_the_package_install_directory(self):
        self.write("ya.make", "SDC_UNION(bundle)\nSDC_INSTALL(LIB PROGRAM_STARTER answer wrapper)\nEND()\n")
        graph = self.graph()
        output = "$(B)/sdc_install/lib/bundle/wrapper"
        generated = [node for node in graph["graph"] if output in node["outputs"] and any("generate_program_starter" in arg for command in node["cmds"] for arg in command["cmd_args"])]
        self.assertEqual(len(generated), 1)
        args = [arg for command in generated[0]["cmds"] for arg in command["cmd_args"]]
        self.assertIn("answer", args)
