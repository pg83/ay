import json
import unittest

import lib
from test_java import contrib
from test_java_features import tree, command
from test_java_tests import junit_tree


class JavaRemainingContractsTest(unittest.TestCase):
    def test_failed_test_compilation_is_skipped_in_keep_going_mode(self):
        # Equality compares strings upstream; an unsupported operator still
        # fails only the active test-compilation submodule.
        files = junit_tree('JUNIT5()\nIF(MODULE_TAG == "JUNIT5_JAR_COMPILATION")\nIF(MODULE_TAG GREATER 0)\nENDIF()\nENDIF()\nEND()\n')
        result = lib.make_process(files, 'proj/sub/ut', '-k', '-t')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('unsupported IF operator "GREATER"', result.stderr)
        self.assertIn('submodule', result.stderr)
        graph = json.loads(result.stdout)
        self.assertFalse(any(o.endswith('.test.cp.jar') for n in graph['graph'] for o in n['outputs']))

    def test_recursive_native_target_builds_java_program(self):
        files = tree('JAVA_SRCS(src/Main.java)', module='JAVA_PROGRAM')
        files['root/ya.make'] = f'LIBRARY()\n{lib.BARE_MODULE}END()\nRECURSE(app)\n'
        files['root/app/ya.make'] = files.pop('app/ya.make')
        files['root/app/src/Main.java'] = files.pop('app/src/Main.java')
        graph = lib.make(files, 'root')
        program = lib.node_by_output(graph, '$(B)/root/app/root-app.run.cp.jar')
        self.assertIn(program['uid'], graph['result'])

    def test_external_library_copies_only_supported_source_files_and_uses_its_jdk(self):
        files = tree('FILES(source.java skipped.kt)\nJDK_VERSION(21)', module='EXTERNAL_JAVA_LIBRARY',
                     **{'app/source.java': 'class Source {}\n', 'app/skipped.kt': 'class Skipped\n',
                        'consumer/ya.make': 'JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(app)\nEND()\n',
                        'build/platform/java/jdk/jdk21/ya.make': lib.resource_lib('JDK21', 21)})
        graph = lib.make(files, 'consumer')
        jar = lib.node_by_output(graph, '$(B)/app/app.jar')
        args = command(jar, 'compile_java.py')
        self.assertEqual(args[args.index('--java-bin') + 1], '$(B)/resources/JDK21/bin/java')
        self.assertIn('$(B)/app/source.java', jar['inputs'])
        self.assertFalse(any('$(B)/app/skipped.kt' in n['outputs'] for n in graph['graph']))

    def test_kotlin_on_jdk24_enables_native_access(self):
        files = tree('WITH_KOTLIN()\nJDK_VERSION(24)\nJAVA_SRCS(src/Main.kt)',
                     **{'build/platform/java/jdk/jdk24/ya.make': lib.resource_lib('JDK24', 24)})
        jar = lib.node_by_output(lib.make(files, 'app'), '$(B)/app/app.jar')
        args = next(c['cmd_args'] for c in jar['cmds'] if '--kotlin' in c['cmd_args'] and any(a.endswith('run_javac.py') for a in c['cmd_args']))
        self.assertIn('--enable-native-access=ALL-UNNAMED', args)

    def test_source_globs_handle_question_marks_repeated_separators_and_unknown_roots(self):
        files = tree('JAVA_SRCS(src//M?in.java ${UNKNOWN}/ignored.java EXCLUDE src/S?ip.java)')
        jar = lib.node_by_output(lib.make(files, 'app'), '$(B)/app/app.jar')
        self.assertIn('$(S)/app/src/Main.java', jar['inputs'])
        self.assertNotIn('$(S)/app/src/Skip.java', jar['inputs'])

    def test_raw_assignments_preserve_unclosed_references(self):
        files = tree("SET(BROKEN '-Dunfinished=${UNFINISHED')\nJAVAC_FLAGS(${BROKEN})\nJAVA_SRCS(src/Main.java)")
        jar = lib.node_by_output(lib.make(files, 'app'), '$(B)/app/app.jar')
        self.assertIn('-Dunfinished=${UNFINISHED', command(jar, 'run_javac.py'))

    def test_repeated_sandbox_output_is_rejected(self):
        files = tree('FROM_SANDBOX(FILE 42 OUT_NOAUTO bundle.jsrc)\nFROM_SANDBOX(FILE 42 OUT_NOAUTO bundle.jsrc)\nJAVA_SRCS(bundle.jsrc)')
        result = lib.make_process(files, 'app', '--sandboxing')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('duplicate producer', result.stderr)
        self.assertIn('bundle.jsrc', result.stderr)

    def test_single_sandbox_output_has_a_single_producer_and_jar_dependency(self):
        files = tree('FROM_SANDBOX(FILE 42 OUT_NOAUTO bundle.jsrc)\nJAVA_SRCS(bundle.jsrc)')
        graph = lib.make(files, 'app', '--sandboxing')
        self.assertEqual(sum('$(B)/app/bundle.jsrc' in n['outputs'] for n in graph['graph']), 1)
        jar = lib.node_by_output(graph, '$(B)/app/app.jar')
        producer = lib.node_by_output(graph, '$(B)/app/bundle.jsrc')
        self.assertIn(producer['uid'], jar['deps'])

    def test_event_proto_imports_reach_the_copy_node(self):
        files = lib.java_tree(**{
            'app/ya.make': 'JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(p)\nEND()\n',
            'p/ya.make': 'PROTO_LIBRARY()\nSRCS(event.ev)\nENABLE(KOTLIN_PROTO)\nDISABLE(KOTLIN_PROTO)\nEND()\n',
            'p/event.ev': 'import "p/dep.proto";\n', 'p/dep.proto': 'syntax = "proto3";\n',
            **contrib('contrib/java/com/google/protobuf/protobuf-java/4.33.0', jar=7)})
        graph = lib.make(files, 'app')
        event = lib.node_by_output(graph, '$(B)/p/event_ev.proto')
        self.assertIn('$(S)/p/dep.proto', event['inputs'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
