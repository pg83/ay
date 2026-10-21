import unittest

import lib
from test_python_cython import python_base
from test_python2 import python2_base, CYTHON_HEADERS, CYTHON_UTILITY


CASES = (
    ("generic/string", "libcpp/string"),
    ("generic/hash", "libcpp/pair"),
    ("generic/hash_set", "libcpp/pair"),
    ("system/types", "libc/stdint"),
)


def fixture(py2=False):
    files = python_base()
    if py2:
        files.update(python2_base())
        files["contrib/tools/cython_py2/cython.py"] = ""
        for name in CYTHON_UTILITY:
            files["contrib/tools/cython_py2/Cython/Utility/" + name] = ""
        for header in CYTHON_HEADERS:
            files[header] = ""
    names = []
    for prefix in ("util", "vendor"):
        for i, (relative, builtin) in enumerate(CASES):
            name = f"{prefix}{i}.pyx"
            names.append(name)
            files["mod/" + name] = "from " + (prefix + "/" + relative).replace("/", ".") + " cimport value\n"
            files[prefix + "/" + relative + ".pxd"] = "from " + builtin.replace("/", ".") + " cimport value\n"
            for tool in ("cython", "cython_py2"):
                files[f"contrib/tools/{tool}/Cython/Includes/{builtin}.pxd"] = ""
    files["mod/ya.make"] = "LIBRARY()\n" + lib.BARE_MODULE + ("USE_PYTHON2()\n" if py2 else "SET(PYTHON3 yes)\n") + "".join("BUILDWITH_CYTHON_CPP(" + name + ")\n" for name in names) + "END()\n"
    return files


class CythonBuiltinResolutionTest(unittest.TestCase):
    def test_source_file_names_do_not_override_the_configured_builtin_search_path(self):
        self.check_context(False)

    def test_python2_context_keeps_its_configured_builtin_search_path(self):
        self.check_context(True)

    def check_context(self, py2):
        graph = lib.make(fixture(py2), "mod", "--sandboxing")
        selected, other = ("cython_py2", "cython") if py2 else ("cython", "cython_py2")
        suffix = ".py2.cpp" if py2 else ".cpp"
        for prefix in ("util", "vendor"):
            for i, (_, builtin) in enumerate(CASES):
                with self.subTest(prefix=prefix, builtin=builtin, i=i):
                    node = lib.node_by_output(graph, f"$(B)/mod/{prefix}{i}.pyx{suffix}")
                    self.assertIn(f"$(S)/contrib/tools/{selected}/Cython/Includes/{builtin}.pxd", node["inputs"])
                    self.assertNotIn(f"$(S)/contrib/tools/{other}/Cython/Includes/{builtin}.pxd", node["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
