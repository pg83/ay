from tst.make.lib import MakeTest


class JavaUnrootedGeneratedSourcesTest(MakeTest):
    def test_generated_files_are_not_unrooted_java_source_directories(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator CLASSPATH {self.project_name}/tool OUT generated)\nJAVA_SRCS(SRCDIR generated *.java)\nEND()\n")
        self.write("tool/ya.make", "JAVA_PROGRAM(generator)\nJDK_VERSION(17)\nJAVA_SRCS(Generator.java)\nEND()\n")
        self.write("tool/Generator.java", 'public class Generator { public static void main(String[] args) throws Exception { java.nio.file.Files.writeString(java.nio.file.Path.of("generated"), "public class Answer {}\\n"); } }\n')
        self.assertNotEqual(self.make("-t").returncode, 0)
