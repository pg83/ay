import unittest

from tst import lib


def fixture(call=None, source='use.cpp', include=True, before='', after=''):
    if call is None:
        call = 'Main generated.h OUT generated.h CLASSPATH grove/tool'
    return lib.java_tree(**{
        'build/platform/java/scriptgen/ya.make': lib.resource_lib('SCRIPTGEN', 9),
        'build/conf/java.conf': 'JDK_REAL_VERSION=17\nJDK_RESOURCE_PEERDIR=build/platform/java/jdk/jdk17\n',
        'grove/tool/ya.make': 'JAVA_PROGRAM(tool)\nNO_LINT()\nJAR_MAIN_CLASS(Main)\nJAVA_SRCS(Main.java)\nEND()\n',
        'grove/tool/Main.java': 'public class Main {}\n',
        'grove/leaf/ya.make': ('LIBRARY(leaf)\n' + lib.BARE_MODULE + before +
                               'RUN_JAVA_PROGRAM(' + call + ')\n' + after +
                               ('SRCS(' + source + ')\n' if source else '') + 'END()\n'),
        'grove/leaf/use.cpp': ('#include <grove/leaf/generated.h>\n' if include else '') + 'int leaf(){return 1;}\n',
        'grove/leaf/seed.txt': 'seed\n',
    })


class NativeJavaProgramTest(unittest.TestCase):
    def assert_graph(self, graph):
        ids = {n['uid'] for n in graph['graph']}
        self.assertTrue(graph['result'])
        self.assertTrue(set(graph['result']) <= ids)
        self.assertTrue(all(set(n['deps']) <= ids for n in graph['graph']))

    def test_header_producer_reaches_cpp_consumer_and_host_java_tool(self):
        graph = lib.make(fixture(), 'grove/leaf')
        self.assert_graph(graph)
        producer = lib.node_by_output(graph, '$(B)/grove/leaf/generated.h')
        tool = lib.node_by_output(graph, '$(B)/grove/tool/tool.run.cp.jar')
        consumer = lib.node_by_output(graph, '$(B)/grove/leaf/use.cpp.o')
        archive = lib.node_by_output(graph, '$(B)/grove/leaf/libleaf.a')
        self.assertEqual(producer['kv']['p'], 'RJ')
        self.assertEqual(producer['platform'], 'default-linux-aarch64')
        self.assertEqual(tool['platform'], 'default-linux-x86_64')
        self.assertIn(tool['uid'], producer['deps'])
        self.assertIn(tool['outputs'][0], producer['inputs'])
        self.assertIn(producer['uid'], consumer['deps'])
        self.assertIn(producer['outputs'][0], consumer['inputs'])
        self.assertIn(consumer['uid'], archive['deps'])
        self.assertIn(archive['uid'], graph['result'])
        self.assertEqual(producer['cmds'][0]['cmd_args'][-1], '$(S)/build/scripts/mkdir.py')
        self.assertEqual(producer['cmds'][1]['cmd_args'][-2:], ['Main', producer['outputs'][0]])
        self.assertEqual(producer['cmds'][2]['cmd_args'][-3:], ['$(S)/build/scripts/autotar_gendirs.py', '--pack', '--outs'])
        self.assertIn('$(B)/resources/JDK17/bin/java', producer['cmds'][1]['cmd_args'])

    def test_input_and_call_site_values_are_retained(self):
        graph = lib.make(fixture('Main $OPTS seed.txt generated.h IN seed.txt OUT_NOAUTO generated.h CLASSPATH grove/tool',
                                before='SET(OPTS --value old)\n', after='SET(OPTS --value new)\n'), 'grove/leaf')
        producer = lib.node_by_output(graph, '$(B)/grove/leaf/generated.h')
        self.assertIn('$(S)/grove/leaf/seed.txt', producer['inputs'])
        self.assertEqual(producer['cmds'][1]['cmd_args'][-5:],
                         ['Main', '--value', 'old', '$(S)/grove/leaf/seed.txt', '$(B)/grove/leaf/generated.h'])
        self.assert_graph(graph)

    def test_generated_input_keeps_its_real_copy_producer_in_both_declaration_orders(self):
        call = 'Main ${BINDIR}/staged.txt generated.h IN ${BINDIR}/staged.txt OUT generated.h CLASSPATH grove/tool'
        for before, after in [('COPY_FILE(seed.txt staged.txt)\n', ''), ('', 'COPY_FILE(seed.txt staged.txt)\n')]:
            with self.subTest(before=before):
                graph = lib.make(fixture(call, before=before, after=after), 'grove/leaf')
                copy = lib.node_by_output(graph, '$(B)/grove/leaf/staged.txt')
                producer = lib.node_by_output(graph, '$(B)/grove/leaf/generated.h')
                self.assertIn(copy['uid'], producer['deps'])
                self.assertIn(copy['outputs'][0], producer['inputs'])
                self.assertNotIn('$(S)/grove/leaf/staged.txt', producer['inputs'])
                self.assert_graph(graph)

    def test_out_dir_identity_and_explicit_header_share_one_action(self):
        graph = lib.make(fixture('Main generated.h OUT generated.h OUT_DIR emitted CLASSPATH grove/tool'), 'grove/leaf')
        producer = lib.node_by_output(graph, '$(B)/grove/leaf/generated.h')
        self.assertEqual(len(producer['outputs']), 2)
        self.assertTrue(producer['outputs'][1].startswith('$(B)/grove/leaf/emitted.'))
        self.assertTrue(producer['outputs'][1].endswith('.gentar'))
        self.assertEqual(producer['cmds'][0]['cmd_args'][-1], 'emitted')
        self.assertEqual(producer['cmds'][2]['cmd_args'][-1], producer['outputs'][1])
        self.assert_graph(graph)

    def test_relative_and_rooted_input_directories_keep_their_source_files(self):
        for directory in ['data', '${CURDIR}/data']:
            with self.subTest(directory=directory):
                files = fixture('Main generated.h IN_DIR ' + directory + ' OUT generated.h CLASSPATH grove/tool')
                files['grove/leaf/data/one.txt'] = 'one\n'
                files['grove/leaf/data/nested/two.txt'] = 'two\n'
                graph = lib.make(files, 'grove/leaf')
                producer = lib.node_by_output(graph, '$(B)/grove/leaf/generated.h')
                self.assertIn('$(S)/grove/leaf/data/one.txt', producer['inputs'])
                self.assertIn('$(S)/grove/leaf/data/nested/two.txt', producer['inputs'])
                self.assert_graph(graph)

    def test_explicit_java_outputs_use_the_same_producer_for_resource_inputs(self):
        files = fixture('Main generated.txt OUT generated.txt CLASSPATH grove/tool')
        files['grove/leaf/ya.make'] = ('JAVA_LIBRARY_NON_SPLIT(bundle)\nNO_LINT()\n'
            'RUN_JAVA_PROGRAM(Main generated.txt OUT generated.txt CLASSPATH grove/tool)\n'
            'JAVA_SRCS(RESOURCES SRCDIR ${BINDIR} FILES ${BINDIR}/generated.txt)\nEND()\n')
        graph = lib.make(files, 'grove/leaf')
        producer = lib.node_by_output(graph, '$(B)/grove/leaf/generated.txt')
        jar = lib.node_by_output(graph, '$(B)/grove/leaf/bundle.jar')
        self.assertIn(producer['outputs'][0], jar['inputs'])
        self.assertIn(producer['uid'], jar['deps'])
        self.assert_graph(graph)

    def test_java_auto_and_noauto_outputs_follow_real_source_consumers(self):
        for keyword, explicit in [('OUT', False), ('OUT_NOAUTO', False), ('OUT_NOAUTO', True)]:
            with self.subTest(keyword=keyword, explicit=explicit):
                files = fixture()
                files['grove/leaf/ya.make'] = ('JAVA_LIBRARY_NON_SPLIT(bundle)\nNO_LINT()\n'
                    'RUN_JAVA_PROGRAM(Main Generated.java ' + keyword + ' Generated.java CLASSPATH grove/tool)\n' +
                    ('JAVA_SRCS(FILES ${BINDIR}/Generated.java)\n' if explicit else '') + 'END()\n')
                graph = lib.make(files, 'grove/leaf')
                jar = lib.node_by_output(graph, '$(B)/grove/leaf/bundle.jar')
                producers = [n for n in graph['graph'] if '$(B)/grove/leaf/Generated.java' in n['outputs']]
                self.assertEqual(len(producers), 1)
                if producers:
                    self.assertIn(producers[0]['uid'], jar['deps'])
                    self.assertIn(producers[0]['outputs'][0], jar['inputs'])
                self.assert_graph(graph)

    def test_auto_cpp_output_and_unused_header_are_selected_by_real_uses(self):
        graph = lib.make(fixture('Main generated.cpp OUT generated.cpp CLASSPATH grove/tool', source='', include=False), 'grove/leaf')
        producer = lib.node_by_output(graph, '$(B)/grove/leaf/generated.cpp')
        consumer = lib.node_by_output(graph, '$(B)/grove/leaf/generated.cpp.o')
        self.assertIn(producer['uid'], consumer['deps'])
        self.assert_graph(graph)
        graph = lib.make(fixture(include=False), 'grove/leaf')
        self.assertFalse(any(n['kv'].get('p') == 'RJ' for n in graph['graph']))
        self.assertFalse(any(o.startswith('$(B)/grove/tool/') for n in graph['graph'] for o in n['outputs']))
        self.assert_graph(graph)
        result = lib.make_process(fixture('Main CLASSPATH grove/tool', include=False), 'grove/leaf')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('RUN_JAVA_PROGRAM requires at least one output', result.stderr)


if __name__ == '__main__':
    unittest.main()
