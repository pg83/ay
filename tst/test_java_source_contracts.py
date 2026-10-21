import unittest

import lib
from test_java_features import tree


class JavaSourceContractsTest(unittest.TestCase):
    def test_java_wildcards_exclusions_and_repeated_separators(self):
        files = tree('JAVA_SRCS(SRCDIR src //*/M?in.java EXCLUDE ${ARCADIA_ROOT}/app/src//skip/M*.java)', **{
            'app/src/keep/Main.java': 'class Main {}\n',
            'app/src/skip/Main.java': 'class Main {}\n',
            'app/src/keep/More.java': 'class More {}\n',
        })
        jar = lib.node_by_output(lib.make(files, 'app'), '$(B)/app/app.jar')
        sources = [p for p in jar['inputs'] if p.endswith('.java')]
        self.assertEqual(sources, ['$(S)/app/src/keep/Main.java'])

    def test_java_optional_macros_and_raw_global_variables(self):
        files = tree('SET_APPEND()\nJAVA_PROTO_PLUGIN(incomplete)\nNO_YMAKE_PYTHON3()\n'
                     'SET(SOURCE_DIR src/${JAVA_PROTO_RUNTIME_VERSION})\n'
                     'JAVA_SRCS(SRCDIR ${SOURCE_DIR} *.java)', **{
            'app/src/4.33.0/Main.java': 'class Main {}\n',
        })
        graph = lib.make(files, 'app')
        jar = lib.node_by_output(graph, '$(B)/app/app.jar')
        self.assertIn('$(S)/app/src/4.33.0/Main.java', jar['inputs'])
        self.assertFalse(any('$(B)/resources/YMAKE_PYTHON3' in n['outputs'] for n in graph['graph']))



if __name__ == '__main__':
    unittest.main(verbosity=2)
