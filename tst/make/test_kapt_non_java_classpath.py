from tst.make.lib import MakeTest, PROJECT


class KaptNonJavaClasspathTest(MakeTest):
    def test_native_archives_are_not_kapt_processor_jars(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nWITH_KOTLIN()\nWITH_KAPT()\nJAVA_SRCS(Answer.kt)\nKAPT_ANNOTATION_PROCESSOR_CLASSPATH({PROJECT}/native)\nEND()\n")
        self.write("Answer.kt", "class Answer\n")
        self.write("native/ya.make", "LIBRARY(native)\nSRCS(answer.cpp)\nEND()\n")
        self.write("native/answer.cpp", "int answer() { return 42; }\n")
        node = self.node(self.graph(), "answer.jar")
        args = next(command["cmd_args"] for command in node["cmds"] if any(arg.endswith("/with_kapt_args.py") for arg in command["cmd_args"]))
        classpath = args[args.index("--ap-classpath") + 1:args.index("--ap-options")]
        self.assertFalse(any(arg.endswith(".a") for arg in classpath))
