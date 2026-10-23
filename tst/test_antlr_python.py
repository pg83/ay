import unittest

import lib
from test_codegen_antlr import antlr_files
from test_python_srcs import python_base


MODULE = 'grove/grammar'
B = '$(B)/' + MODULE + '/'
S = '$(S)/' + MODULE + '/'
DEFAULTS = '''_ANTLR4_11_1_JAR=contrib/java/antlr/antlr4_11/antlr.jar
_ANTLR4_11_1_PYTHON_RUNTIME=contrib/python/antlr4
_ANTLR4_JAR=contrib/java/antlr/antlr4/antlr.jar
_ANTLR4_PYTHON_RUNTIME=contrib/python/antlr4
'''


def fixture(version, options='', consume=True, direct=False):
    files = python_base()
    files.update(antlr_files('', {}))
    del files['mod/ya.make']
    files['build/ymake.core.conf'] = lib.CONF_FILES['build/ymake.core.conf'] + DEFAULTS
    files['contrib/java/antlr/antlr4_11/antlr.jar'] = ''
    files['contrib/java/antlr/antlr4/antlr.jar'] = ''
    lib.tool_program(files, 'tools/py2cc', 'py2cc')
    files['contrib/python/antlr4/ya.make'] = 'PY23_LIBRARY()\n' + lib.BARE_MODULE + 'NO_PYTHON_INCLUDES()\nCFLAGS(GLOBAL -DANTLR_RUNTIME)\nEND()\n'
    files[MODULE + '/schemas/Moth.g4'] = 'grammar Moth;\nentry: ID;\nID: [a-z]+;\n'
    files[MODULE + '/anchor.py'] = ''
    files[MODULE + '/anchor.cpp'] = 'int anchor;\n'
    files[MODULE + '/ya.make'] = ('PY' + str(version) + '_LIBRARY()\n' + lib.BARE_MODULE
                                 + 'NO_PYTHON_INCLUDES()\nPY_SRCS(anchor.py)\nSRCS(anchor.cpp)\n'
                                 + 'RUN_ANTLR4_PYTHON' + str(version) + '(schemas/Moth.g4 ' + options + ')\n')
    if consume:
        out = 'nested/out/MothParser.py' if 'SUBDIR' in options else 'MothParser.py'
        files[MODULE + '/ya.make'] += ('PY_SRCS(' + out + ')\n' if direct
                                     else 'COPY_FILE(' + out + ' brought.py)\nPY_SRCS(brought.py)\n')
    files[MODULE + '/ya.make'] += 'END()\n'
    return files


def grammar_node(graph, base='Moth', subdir=''):
    return lib.node_by_output(graph, B + subdir + base + 'Lexer.py')


class AntlrPythonTest(unittest.TestCase):
    def check(self, version, options='', subdir='', suffixes=('Lexer.py', 'Parser.py'), direct=False):
        graph = lib.make(fixture(version, options, direct=direct), MODULE, '--target-platform', 'default-linux-x86_64')
        node = grammar_node(graph, subdir=subdir)
        jar = '$(S)/contrib/java/antlr/' + ('antlr4_11' if version == 2 else 'antlr4') + '/antlr.jar'
        self.assertEqual(node['outputs'], [B + subdir + 'Moth' + x for x in suffixes])
        flags = ['-visitor' if 'VISITOR' in options else '-no-visitor', '-listener' if 'LISTENER' in options else '-no-listener']
        self.assertEqual(node['cmds'][0]['cmd_args'][1:], [
            '$(S)/build/scripts/stdout2stderr.py', '$(B)/resources/JDK17/bin/java', '-jar', jar,
            S + 'schemas/Moth.g4', '-Dlanguage=Python' + str(version), '-o', B + (subdir.rstrip('/') if subdir else '.'), *flags,
            *(['-package', 'moth'] if '-package' in options else []),
        ])
        self.assertEqual(node['cmds'][0]['cwd'], B.rstrip('/'))
        self.assertEqual(node['inputs'], [S + 'schemas/Moth.g4', '$(S)/build/scripts/stdout2stderr.py', jar])
        cc = lib.node_by_output(graph, B + 'anchor.cpp.o')
        self.assertIn('-DANTLR_RUNTIME', cc['cmds'][0]['cmd_args'])
        if not direct:
            copied = lib.node_by_output(graph, B + 'brought.py')
            self.assertIn(node['uid'], copied['deps'])
            self.assertIn(B + subdir + 'MothParser.py', copied['inputs'])
            self.assertIn(node['outputs'][0], copied['inputs'])
        else:
            consumers = [n for n in graph['graph'] if B + subdir + 'MothParser.py' in n['inputs']]
            self.assertTrue(consumers)
            self.assertTrue(any(node['uid'] in n['deps'] for n in consumers))
        self.assertTrue(graph['result'])
        return graph

    def test_both_python_versions_use_their_source_defined_jars(self):
        for version in (2, 3):
            self.check(version)

    def test_subdir_optional_outputs_and_tool_options_keep_native_order(self):
        for version in (2, 3):
            self.check(version, 'LISTENER VISITOR -package moth SUBDIR nested/out EXTRA_OUTS Moth.tokens Moth.interp',
                       'nested/out/', ('Lexer.py', 'Parser.py', 'Listener.py', 'Visitor.py', '.tokens', '.interp'))

    def test_ordinary_python_sources_can_consume_noauto_outputs_explicitly(self):
        self.check(3, 'VISITOR SUBDIR nested/out', 'nested/out/', ('Lexer.py', 'Parser.py', 'Visitor.py'), direct=True)

    def test_unused_noauto_outputs_are_not_automatic_python_sources(self):
        graph = lib.make(fixture(3, 'SUBDIR untouched', consume=False), MODULE)
        self.assertFalse(any(n['kv'].get('p') == 'JV' for n in graph['graph']))
        self.assertFalse(any('/untouched/' in i for n in graph['graph'] for i in n['inputs']))
        self.assertIn('-DANTLR_RUNTIME', lib.node_by_output(graph, B + 'anchor.cpp.o')['cmds'][0]['cmd_args'])

    def test_generated_grammar_and_extra_input_keep_real_producers(self):
        files = fixture(2, 'LISTENER SUBDIR nested/out')
        files[MODULE + '/template.g4'] = files.pop(MODULE + '/schemas/Moth.g4')
        files[MODULE + '/ya.make'] = files[MODULE + '/ya.make'].replace(
            'RUN_ANTLR4_PYTHON2(schemas/Moth.g4', 'COPY_FILE(template.g4 schemas/Moth.g4)\nRUN_ANTLR4_PYTHON2(schemas/Moth.g4')
        graph = lib.make(files, MODULE)
        generated = lib.node_by_output(graph, B + 'schemas/Moth.g4')
        node = grammar_node(graph, subdir='nested/out/')
        self.assertIn(generated['uid'], node['deps'])
        self.assertIn(generated['outputs'][0], node['inputs'])
        self.assertIn(generated['outputs'][0], node['cmds'][0]['cmd_args'])
        files = fixture(3, 'IN side.g4')
        files[MODULE + '/side.g4'] = 'parser grammar Side;\n'
        graph = lib.make(files, MODULE)
        self.assertEqual(grammar_node(graph)['inputs'], [S + 'schemas/Moth.g4', '$(S)/build/scripts/stdout2stderr.py',
                                                         '$(S)/contrib/java/antlr/antlr4/antlr.jar', S + 'side.g4'])
        self.assertNotIn('IN', grammar_node(graph)['cmds'][0]['cmd_args'])

    def test_explicit_source_config_bindings_select_jar_and_runtime(self):
        files = fixture(3)
        files[MODULE + '/ya.make'] = files[MODULE + '/ya.make'].replace(
            'RUN_ANTLR4_PYTHON3', 'SET(_ANTLR4_JAR ${ARCADIA_ROOT}/private/runner.jar)\n'
            'SET(_ANTLR4_PYTHON_RUNTIME private/runtime)\nRUN_ANTLR4_PYTHON3')
        files['private/runner.jar'] = ''
        files['private/runtime/ya.make'] = 'PY23_LIBRARY()\n' + lib.BARE_MODULE + 'NO_PYTHON_INCLUDES()\nCFLAGS(GLOBAL -DPRIVATE_RUNTIME)\nEND()\n'
        graph = lib.make(files, MODULE)
        node = grammar_node(graph)
        self.assertIn('$(S)/private/runner.jar', node['inputs'])
        self.assertIn('$(S)/private/runner.jar', node['cmds'][0]['cmd_args'])
        args = lib.node_by_output(graph, B + 'anchor.cpp.o')['cmds'][0]['cmd_args']
        self.assertIn('-DPRIVATE_RUNTIME', args)
        self.assertNotIn('-DANTLR_RUNTIME', args)

    def test_source_variables_bind_options_before_macro_arguments(self):
        for version in (2, 3):
            for prefix, call in (
                ('SET(OPTS VISITOR SUBDIR trees)\n', 'schemas/Moth.g4 ${OPTS}'),
                ('SET(GRAMMAR schemas/Moth.g4 VISITOR SUBDIR trees)\n', '${GRAMMAR}'),
            ):
                files = fixture(version)
                files[MODULE + '/ya.make'] = files[MODULE + '/ya.make'].replace(
                    'RUN_ANTLR4_PYTHON' + str(version) + '(schemas/Moth.g4 )',
                    prefix + 'RUN_ANTLR4_PYTHON' + str(version) + '(' + call + ')').replace(
                    'COPY_FILE(MothParser.py', 'COPY_FILE(trees/MothParser.py')
                graph = lib.make(files, MODULE)
                node = grammar_node(graph, subdir='trees/')
                self.assertEqual(node['outputs'], [B + 'trees/Moth' + x for x in ('Lexer.py', 'Parser.py', 'Visitor.py')])
                self.assertEqual(node['cmds'][0]['cmd_args'][-4:], ['-o', B + 'trees', '-visitor', '-no-listener'])
                self.assertEqual(node['inputs'][0], S + 'schemas/Moth.g4')
                self.assertIn(node['uid'], lib.node_by_output(graph, B + 'brought.py')['deps'])
                self.assertTrue(graph['result'])

    def test_bare_variables_expand_after_binding_and_output_name_modifiers(self):
        for grammar, options, outputbase, tail in (
            ('${GRAMMAR}', 'VISITOR', 'Moth', ['-visitor', '-no-listener']),
            ('$GRAMMAR', 'VISITOR', '$GRAMMAR', ['-visitor', '-no-listener']),
            ('schemas/Moth.g4', '$OPTS', 'Moth', ['-no-visitor', '-no-listener', 'VISITOR', 'SUBDIR', 'trees']),
        ):
            files = fixture(3)
            files[MODULE + '/ya.make'] = files[MODULE + '/ya.make'].replace(
                'RUN_ANTLR4_PYTHON3(schemas/Moth.g4 )',
                'SET(GRAMMAR schemas/Moth.g4)\nSET(OPTS VISITOR SUBDIR trees)\n'
                + 'RUN_ANTLR4_PYTHON3(' + grammar + ' ' + options + ')').replace(
                'COPY_FILE(MothParser.py', 'COPY_FILE(' + outputbase + 'Parser.py')
            graph = lib.make(files, MODULE)
            node = grammar_node(graph, base=outputbase)
            self.assertEqual(node['inputs'][0], S + 'schemas/Moth.g4')
            self.assertEqual(node['cmds'][0]['cmd_args'][-len(tail):], tail)
            self.assertEqual(node['outputs'], [B + outputbase + x for x in
                                             (('Lexer.py', 'Parser.py', 'Visitor.py') if options == 'VISITOR' else ('Lexer.py', 'Parser.py'))])
            self.assertIn(node['uid'], lib.node_by_output(graph, B + 'brought.py')['deps'])

    def test_variable_output_lists_subdir_and_empty_grammar(self):
        for reference in ('$OUTS', '${OUTS}'):
            files = fixture(3)
            files[MODULE + '/ya.make'] = files[MODULE + '/ya.make'].replace(
                'RUN_ANTLR4_PYTHON3(schemas/Moth.g4 )',
                'SET(OUTS Moth.tokens Moth.interp)\nSET(DIR trees)\n'
                + 'RUN_ANTLR4_PYTHON3(schemas/Moth.g4 SUBDIR $DIR EXTRA_OUTS ' + reference + ')').replace(
                'COPY_FILE(MothParser.py', 'COPY_FILE(trees/MothParser.py')
            graph = lib.make(files, MODULE)
            self.assertEqual(grammar_node(graph, subdir='trees/')['outputs'],
                             [B + 'trees/Moth' + x for x in ('Lexer.py', 'Parser.py', '.tokens', '.interp')])
        files = fixture(3, consume=False)
        files[MODULE + '/ya.make'] = files[MODULE + '/ya.make'].replace(
            'RUN_ANTLR4_PYTHON3(schemas/Moth.g4 )', 'SET(GRAMMAR)\nRUN_ANTLR4_PYTHON3(${GRAMMAR})')
        result = lib.make_process(files, MODULE)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('expects a grammar', result.stderr)

    def test_missing_grammar_and_subdir_value_report_errors(self):
        for declaration, message in (('RUN_ANTLR4_PYTHON3()', 'expects a grammar'),
                                     ('RUN_ANTLR4_PYTHON2(schemas/Moth.g4 SUBDIR)', 'SUBDIR expects a value')):
            files = fixture(3, consume=False)
            files[MODULE + '/ya.make'] = 'PY3_LIBRARY()\n' + lib.BARE_MODULE + declaration + '\nEND()\n'
            result = lib.make_process(files, MODULE)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(message, result.stderr)


if __name__ == '__main__':
    unittest.main()
