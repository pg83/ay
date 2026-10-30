from tst.make.lib import MakeTest, PROJECT


class ConfigureFileScannerTest(MakeTest):
    def test_command_line_values_override_declared_template_defaults(self):
        self.write("ya.make", "LIBRARY()\nDEFAULT(E2E_VALUE 17)\nCONFIGURE_FILE(config.in result.txt)\nRESOURCE(result.txt /result)\nEND()\n")
        self.write("config.in", "@E2E_VALUE@\n")
        configured = self.node(self.graph("-DE2E_VALUE=42"), "result.txt")
        args = [arg for command in configured["cmds"] for arg in command["cmd_args"]]
        self.assertIn("E2E_VALUE=42", args)
        self.assertNotIn("E2E_VALUE=17", args)

    def test_copied_templates_keep_their_producer_when_configured(self):
        self.write("config.in", "@E2E_VALUE@\n")
        for macro in ("COPY_FILE", "COPY_FILE_WITH_CONTEXT"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\nSET(E2E_VALUE 42)\n{macro}(config.in copied.in)\nCONFIGURE_FILE(copied.in result.txt)\nRESOURCE(result.txt /result)\nEND()\n")
                graph = self.graph()
                copied = self.node(graph, "copied.in")
                configured = self.node(graph, "result.txt")
                self.assertIn(f"$(S)/{PROJECT}/config.in", copied["inputs"])
                self.assertIn(f"$(B)/{PROJECT}/copied.in", configured["inputs"])
                self.assertIn(copied["uid"], configured["deps"])

    def test_chained_template_copies_keep_each_producer(self):
        self.write("config.in", "answer = 42\n")
        self.write("ya.make", "LIBRARY()\nCOPY_FILE(config.in first.in)\nCOPY_FILE(first.in second.in)\nRESOURCE(second.in /template)\nEND()\n")
        graph = self.graph()
        first = self.node(graph, "first.in")
        second = self.node(graph, "second.in")
        self.assertIn(f"$(B)/{PROJECT}/first.in", second["inputs"])
        self.assertIn(first["uid"], second["deps"])

    def test_referenced_variables_and_cmake_definitions(self):
        self.write("ya.make", "LIBRARY()\nSET(E2E_VALUE 42)\nSET(E2E_ON yes)\nSET(_E2E_12 underscore)\nDEFAULT(E2E_DEFAULT 17)\nSET(E2E_UNUSED unused)\nCONFIGURE_FILE(config.in result.txt)\nRESOURCE(result.txt /result)\nEND()\n")
        self.write("config.in", "@E2E_VALUE@ @_E2E_12@ @E2E_DEFAULT@ @E2E_VALUE@\n#cmakedefine E2E_ON\n#cmakedefine01\tE2E_ON\n")
        node = self.node(self.graph(), "result.txt")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        for value in ("E2E_VALUE=42", "_E2E_12=underscore", "E2E_DEFAULT=17", "E2E_ON=yes"):
            self.assertEqual(args.count(value), 1)
        self.assertNotIn("E2E_UNUSED=unused", args)
        self.assertIn(f"$(S)/{PROJECT}/config.in", node["inputs"])

    def test_truncated_template_tokens_defer_validation_to_execution(self):
        fragments = ("@", "@E", "@E2E_12", "@@", "@!@", "@BAD NAME@",
                     "#cmakedefine", "#cmakedefine ", "#cmakedefine\t", "#cmakedefine0",
                     "#cmakedefine01", "#cmakedefine01 ", "#cmakedefine01\t",
                     "#cmakedefine02 E2E_VALUE", "#cmakedefine01! E2E_VALUE")
        commands = ["LIBRARY()", "SET(E2E_VALUE 42)"]
        for index, fragment in enumerate(fragments):
            self.write(f"template{index}.in", "@E2E_VALUE@\n" + fragment)
            commands.extend((f"CONFIGURE_FILE(template{index}.in result{index}.txt)",
                             f"RESOURCE(result{index}.txt /result{index})"))
        self.write("ya.make", "\n".join((*commands, "END()", "")))
        graph = self.graph()
        for index in range(len(fragments)):
            with self.subTest(fragment=fragments[index]):
                node = self.node(graph, f"result{index}.txt")
                args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
                self.assertIn("E2E_VALUE=42", args)
                self.assertIn(f"$(S)/{PROJECT}/template{index}.in", node["inputs"])

    def test_lowercase_and_hyphenated_variable_names(self):
        self.write("ya.make", "LIBRARY()\nSET(e2e_lower lower)\nSET(E2E-DASH dash)\nCONFIGURE_FILE(config.in result.txt)\nRESOURCE(result.txt /result)\nEND()\n")
        self.write("config.in", "@e2e_lower@\n@E2E-DASH@\n")
        args = [arg for command in self.node(self.graph(), "result.txt")["cmds"] for arg in command["cmd_args"]]
        self.assertIn("e2e_lower=lower", args)
        self.assertIn("E2E-DASH=-DASH", args)
