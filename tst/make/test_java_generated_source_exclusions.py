from tst.make.lib import MakeTest, PROJECT


class JavaGeneratedSourceExclusionsTest(MakeTest):
    def test_generated_source_patterns_preserve_exclusions(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nENABLE(SOURCES_JAR)\nRUN_JAVA_PROGRAM(Generator CLASSPATH {PROJECT}/tool OUT_DIR generated)\nJAVA_SRCS(SRCDIR ${{BINDIR}}/generated **/*.java EXCLUDE **/Ignored.java)\nEND()\n")
        self.write("tool/ya.make", "JAVA_PROGRAM(generator)\nJDK_VERSION(17)\nJAVA_SRCS(Generator.java)\nEND()\n")
        self.write("tool/Generator.java", 'public class Generator { public static void main(String[] args) throws Exception { java.nio.file.Files.createDirectories(java.nio.file.Path.of("generated")); java.nio.file.Files.writeString(java.nio.file.Path.of("generated/Answer.java"), "public class Answer {}\\n"); } }\n')
        node = self.node(self.graph(), "answer.jar")
        prepare = next(command["cmd_args"] for command in node["cmds"] if any(arg.endswith("/prepare_jar_build.py") for arg in command["cmd_args"]))
        self.assertIn(f"$(B)/{PROJECT}/answer-sources.jar", node["outputs"])
        excludes = [prepare[i + 1] for i, arg in enumerate(prepare) if arg == "--exclude-patterns"]
        self.assertEqual(excludes, ["**/Ignored.java", "**/Ignored.java"])
