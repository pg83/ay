from tst.make.lib import MakeTest, PROJECT


class CythonScannerBoundariesTest(MakeTest):
    def setUp(self):
        super().setUp()
        self.write("ya.make", f"PY3_LIBRARY()\nADDINCL({PROJECT})\nPY_SRCS(answer.pyx)\nEND()\n")

    def test_raw_cdef_header_strings_keep_the_header_dependency(self):
        self.write("answer.pyx", 'cdef extern from r"answer.h":\n    int answer()\n')
        self.write("answer.h", "int answer();\n")
        compiled = self.node(self.graph(), "answer.pyx.cpp.o")
        self.assertIn(f"$(S)/{PROJECT}/answer.h", compiled["inputs"])

    def test_relative_cimports_keep_package_dependencies(self):
        self.write("answer.pyx", "from . cimport dependency\ndef answer(): return dependency.value\n")
        self.write("__init__.pxd", "\n")
        self.write("dependency.pxd", "cdef int value\n")
        generated = self.node(self.graph(), "answer.pyx.cpp")
        self.assertIn(f"$(S)/{PROJECT}/dependency.pxd", generated["inputs"])

    def test_continued_cimport_lists_keep_each_dependency(self):
        self.write("answer.pyx", "cimport first, \\\n    second\ndef answer(): return 42\n")
        for name in ("first", "second"):
            self.write(name + ".pxd", "cdef int value\n")
        generated = self.node(self.graph(), "answer.pyx.cpp")
        for name in ("first", "second"):
            self.assertIn(f"$(S)/{PROJECT}/{name}.pxd", generated["inputs"])

    def test_truncated_import_clauses_keep_complete_earlier_imports(self):
        self.write("dependency.pxd", "cdef int value\n")
        clauses = ("from .", "from dependency", "from dependency cimport", "cimport ,",
                   "cimport dependency,", "from dependency cimport (value", "cimport dependency trailing",
                   "cimport dependency # unfinished")
        for clause in clauses:
            with self.subTest(clause=clause):
                self.write("answer.pyx", "cimport dependency\n" + clause + "\n")
                generated = self.node(self.graph(), "answer.pyx.cpp")
                self.assertIn(f"$(S)/{PROJECT}/dependency.pxd", generated["inputs"])

    def test_unterminated_cdef_headers_do_not_hide_complete_imports(self):
        self.write("dependency.pxd", "cdef int value\n")
        for target in ('"<missing.h', '"<missing.h>', '"missing.h', '"missing<h"'):
            with self.subTest(target=target):
                self.write("answer.pyx", "cimport dependency\ncdef extern from " + target + ":\n    pass\n")
                generated = self.node(self.graph(), "answer.pyx.cpp")
                self.assertIn(f"$(S)/{PROJECT}/dependency.pxd", generated["inputs"])

    def test_incomplete_module_names_keep_complete_earlier_imports(self):
        self.write("dependency.pxd", "cdef int value\n")
        for clause in ("from ( cimport value\n", "cimport dependency, \\\n    dependency"):
            with self.subTest(clause=clause):
                self.write("answer.pyx", "cimport dependency\n" + clause)
                generated = self.node(self.graph(), "answer.pyx.cpp")
                self.assertIn(f"$(S)/{PROJECT}/dependency.pxd", generated["inputs"])

    def test_quoted_angle_include_names_use_the_literal_file_name(self):
        self.write("answer.pyx", 'include "<included.pxi>"\ndef answer(): return 42\n')
        for name in ("included.pxi", "<included.pxi>"):
            self.write(name, "cdef int value\n")
        generated = self.node(self.graph(), "answer.pyx.cpp")
        self.assertIn(f"$(S)/{PROJECT}/<included.pxi>", generated["inputs"])

    def test_empty_include_names_keep_complete_imports_with_keep_going(self):
        self.write("dependency.pxd", "cdef int value\n")
        for target in ('""', '"<>"'):
            with self.subTest(target=target):
                self.write("answer.pyx", "cimport dependency\ninclude " + target + "\ndef answer(): return 42\n")
                generated = self.node(self.graph("-k"), "answer.pyx.cpp")
                self.assertIn(f"$(S)/{PROJECT}/dependency.pxd", generated["inputs"])
