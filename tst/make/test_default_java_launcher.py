from tst.make.lib import MakeTest, PROJECT


class DefaultJavaLauncherTest(MakeTest):
    def test_generate_script_uses_the_default_template(self):
        self.write("ya.make", "JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nGENERATE_SCRIPT(OUT launch.sh)\nEND()\n")
        self.write("Main.java", "public class Main { public static void main(String[] args) {} }\n")
        self.write("template.tmpl", "#!/bin/sh\necho answer\n")
        node = self.node(self.graph(), "launch.sh")
        self.assertIn(f"$(S)/{PROJECT}/template.tmpl", node["inputs"])
