from tst.make.lib import MakeTest


class EmptyIncludePathsTest(MakeTest):
    def test_empty_include_paths_are_configuration_errors(self):
        self.write("ya.make", 'LIBRARY()\nINCLUDE("")\nEND()\n')
        self.assertNotEqual(self.make().returncode, 0)
