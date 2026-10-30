from tst.make.lib import MakeTest, PROJECT


class JavaRuntimeOptionsTest(MakeTest):
    def test_preview_is_enabled_in_generated_launchers(self):
        self.write("ya.make", "JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nENABLE_PREVIEW()\nJAVA_SRCS(Main.java)\nEND()\n")
        self.write("Main.java", "public class Main { public static void main(String[] args) {} }\n")
        launcher = self.node(self.graph(), "answer.run.cp.jar")
        args = [arg for command in launcher["cmds"] for arg in command["cmd_args"]]
        self.assertIn("ENABLE_PREVIEW=yes", args)

    def test_generator_accepts_the_sources_classpath_flag(self):
        self.generator()
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator ${{BINDIR}}/Generated.java ADD_SRCS_TO_CLASSPATH CLASSPATH {PROJECT}/tool OUT Generated.java)\nEND()\n")
        graph = self.graph()
        generated = self.node(graph, "Generated.java")
        self.assertIn(generated["uid"], self.node(graph, "answer.jar")["deps"])
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertIn("Generator", args)
        self.assertNotIn("ADD_SRCS_TO_CLASSPATH", args)

    def test_relative_generator_cwd_keeps_the_requested_path(self):
        self.generator()
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator ${{BINDIR}}/Generated.java CLASSPATH {PROJECT}/tool CWD work OUT Generated.java)\nEND()\n")
        self.write("work/value.txt", "42\n")
        node = self.node(self.graph(), "Generated.java")
        command = next(command for command in node["cmds"] if "Generator" in command["cmd_args"])
        self.assertEqual(command["cwd"], "work")

    def generator(self):
        self.write("tool/ya.make", "JAVA_PROGRAM(generator)\nJDK_VERSION(17)\nJAVA_SRCS(Generator.java)\nEND()\n")
        self.write("tool/Generator.java", 'public class Generator { public static void main(String[] args) throws Exception { java.nio.file.Files.writeString(java.nio.file.Path.of(args[0]), "public class Generated {}\\n"); } }\n')
