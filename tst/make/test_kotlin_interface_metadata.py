from tst.make.lib import MakeTest, PROJECT


class KotlinInterfaceMetadataTest(MakeTest):
    def test_kotlin_interface_jars_keep_full_jar_metadata(self):
        self.write("ya.make", f"JAVA_LIBRARY(consumer)\nJDK_VERSION(17)\nJAVA_SRCS(Consumer.java)\nPEERDIR({PROJECT}/library)\nEND()\n")
        self.write("Consumer.java", "public class Consumer {}\n")
        self.write("library/ya.make", "JAVA_LIBRARY(library)\nJDK_VERSION(17)\nWITH_KOTLIN()\nENABLE_KOTLIN_ABI_JAR()\nJAVA_SRCS(Value.kt)\nEND()\n")
        self.write("library/Value.kt", "class Value(val answer: Int = 42)\n")
        graph = self.graph()
        interface = self.node(graph, "library/library-interface.jar")
        args = [arg.replace("$(BUILD_ROOT)", "$(B)") for command in interface["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"$(B)/{PROJECT}/library/library-stripped.jar", args)
        self.assertIn(f"$(B)/{PROJECT}/library/library.jar", args)
        self.assertIn(interface["uid"], self.node(graph, "consumer.jar")["deps"])
