from tst.make.lib import MakeTest, PROJECT


class JavaPeerCyclesTest(MakeTest):
    def test_java_peer_cycles_are_configuration_errors(self):
        self.write("ya.make", "RECURSE(first)\n")
        for name, peer in (("first", "second"), ("second", "first")):
            self.write(name + "/ya.make", f"JAVA_LIBRARY({name})\nJDK_VERSION(17)\nJAVA_SRCS(Value.java)\nPEERDIR({PROJECT}/{peer})\nEND()\n")
            self.write(name + "/Value.java", "public class Value {}\n")
        self.assertNotEqual(self.make().returncode, 0)
