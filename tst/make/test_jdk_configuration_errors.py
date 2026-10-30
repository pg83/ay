from tst.make.lib import MakeTest


class JdkConfigurationErrorsTest(MakeTest):
    def test_jdk_selection_requires_one_argument(self):
        for macro in ("JDK_VERSION", "DEFAULT_JDK_VERSION"):
            for args in ("", "17 21"):
                with self.subTest(macro=macro, args=args):
                    self.write("ya.make", f"LIBRARY(answer)\n{macro}({args})\nEND()\n")
                    self.assertNotEqual(self.make().returncode, 0)

    def test_unsupported_jdk_selection_is_rejected(self):
        for macro in ("JDK_VERSION", "DEFAULT_JDK_VERSION"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY(answer)\n{macro}(8)\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)
