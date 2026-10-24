"""Ordinary SRCS SWIG uses the native module recipe and final language."""
import collections
import unittest

import lib
from test_python_swig import python_base, SWIG_LIB


def fixture(case):
    module={'py2module-version':'PY2MODULE(Cedar 2 1 PREFIX lib)','dll-prefix-first':'DLL(PREFIX _ Cedar)','program-default':'PROGRAM(Cedar)','library-unnamed':'LIBRARY()'}.get(case,'LIBRARY(Cedar)')
    src='../api.swg'
    body=''
    tail=''
    if case=='explicit-python':body='SET(SWIG_LANG python)\n'
    if case=='late-python':tail='SET(SWIG_LANG python)\n'
    if case=='source-empty':tail='SET(SWIG_LANG)\n'
    if case in ['late-perl','perl-subdir']:tail='SET(SWIG_LANG perl)\n'
    if case=='generated-source':
        body='COPY_FILE(../api.swg generated.swg)\n'
        src='${BINDIR}/generated.swg'
    if case in ['subdir-source','perl-subdir']:src='sub/api.swg'
    files=python_base()
    for language in ['go','java','perl5']:
        files.pop(SWIG_LIB+'/'+language+'.swg')
        files[SWIG_LIB+'/'+language+'/'+language+'.swg']=''
    files['build/sysincl/macro.yml']='- includes:\n'+''.join('  - '+language+'.swg:\n    - '+SWIG_LIB+'/'+language+'/'+language+'.swg\n' for language in ['go','java','perl5','python'])
    files.update({'bindings/python/ya.make':module+'\n'+lib.BARE_MODULE+body+'SRCS('+src+')\n'+tail+'END()\n',
        'bindings/api.swg':'%module source_name\n%include "api.i"\n',
        'bindings/api.i':'%include "detail.i"\n',
        'bindings/detail.i':'%include "api.i"\n%header %{\n#include "bindings/api.h"\n%}\n',
        'bindings/api.h':'#include "detail.h"\n',
        'bindings/detail.h':'#include "api.h"\n'})
    if case in ['subdir-source','perl-subdir']:
        files['bindings/python/sub/api.swg']=files['bindings/api.swg']
        files['bindings/python/sub/api.i']=files['bindings/api.i']
        files['bindings/python/sub/detail.i']=files['bindings/detail.i']
    if case in ['cli-perl','late-perl','perl-subdir']:
        files[SWIG_LIB+'/perl5/ya.make']='LIBRARY()\n'+lib.BARE_MODULE+'ADDINCL(GLOBAL FOR swig '+SWIG_LIB+'/perl5 GLOBAL FOR swig '+SWIG_LIB+')\nEND()\n'
        files['build/platform/perl/ya.make']='LIBRARY()\n'+lib.BARE_MODULE+'CFLAGS(GLOBAL -DPERL_SDK)\nEND()\n'
    lib.tool_program(files,'tools/fix_elf','fix_elf')
    return files

def flags(case):
    return ['-DSWIG_LANG=perl'] if case in ['late-python','cli-perl'] else ['-DSWIG_LANG='] if case=='cli-empty' else []


class NativePythonSwigTest(unittest.TestCase):
    def make(self, case):
        graph = lib.make(fixture(case), 'bindings/python', '-k', '--ignore-nodes-exit-code', '--add-modules-to-results',
                         '--target-platform', 'default-linux-x86_64', *flags(case))
        outputs = {output for node in graph['graph'] for output in node['outputs']}
        stem = 'generated' if case == 'generated-source' else '_/sub/api' if case in ['subdir-source', 'perl-subdir'] else '__/api'
        wrapper = '$(B)/bindings/python/' + stem + '_wrap.swg.cpp'
        self.assertIn(wrapper, outputs)
        ids = {node['uid'] for node in graph['graph']}
        self.assertTrue(graph['result'])
        self.assertLessEqual(set(graph['result']), ids)
        self.assertTrue(all(set(node['deps']) <= ids for node in graph['graph']))
        return graph, wrapper

    def assert_recipe(self, case, graph, wrapper):
        perl = case in ['cli-perl', 'late-perl', 'perl-subdir']
        name = 'bindings-python' if case == 'library-unnamed' else 'Cedar'
        interface = name if case == 'program-default' else '_' + name if case == 'dll-prefix-first' else 'lib' + name
        module = '$(B)/bindings/python/' + name + ('.pm' if perl else '.py')
        node = lib.node_by_output(graph, wrapper)
        self.assertEqual(node['outputs'], [wrapper, module])
        prefix = ['$(B)/contrib/tools/swig/swig']
        if perl:
            prefix += ['-c++', '-cpperraswarn', '-module', name, '-shadow', '-perl']
        else:
            prefix += ['-module', name, '-cpperraswarn', '-c++', '-python', '-interface', interface]
        source = ('$(B)/bindings/python/generated.swg' if case == 'generated-source' else
                  '$(S)/bindings/python/sub/api.swg' if case in ['subdir-source', 'perl-subdir'] else '$(S)/bindings/api.swg')
        language = 'perl5' if perl else 'python'
        expected = prefix + ['-o', wrapper, '-outdir', '$(B)/bindings/python', '-I$(B)', '-I$(S)',
                             '-I$(S)/' + SWIG_LIB + '/' + language, '-I$(S)/' + SWIG_LIB, source]
        self.assertEqual(node['cmds'][0]['cmd_args'], expected)
        self.assertEqual(node['env'], {'ARCADIA_ROOT_DISTBUILD': '$(S)'})
        self.assertEqual(node['cmds'][0]['env'], node['env'])
        self.assertEqual(node['kv']['p'], 'SW')
        inputs = ['$(B)/contrib/tools/swig/swig', source]
        if case != 'generated-source':
            directory = 'bindings/python/sub/' if case in ['subdir-source', 'perl-subdir'] else 'bindings/'
            inputs += ['$(S)/' + directory + name for name in ['api.i', 'detail.i']]
            inputs += ['$(S)/' + SWIG_LIB + '/' + name for name in [
                'go/go.swg', 'java/java.swg', 'perl5/perl5.swg', 'python/python.swg', 'python/pyhead.swg', 'swig.swg']]
        self.assertEqual(collections.Counter(node['inputs']), collections.Counter(inputs))
        tool = lib.node_by_output(graph, '$(B)/contrib/tools/swig/swig')
        self.assertIn(tool['uid'], node['deps'])
        suffix = '.pic.o' if case in ['py2module-version', 'dll-prefix-first'] else '.o'
        object_path = wrapper + suffix
        if case in ['subdir-source', 'perl-subdir']:
            object_path = object_path.replace('/_/sub/', '/_/_/sub/')
        compiled = lib.node_by_output(graph, object_path)
        self.assertIn(wrapper, compiled['inputs'])
        self.assertIn(node['uid'], compiled['deps'])
        for header in ['$(S)/bindings/api.h', '$(S)/bindings/detail.h']:
            self.assertEqual(header in compiled['inputs'], case != 'generated-source')
            self.assertNotIn(header, node['inputs'])
        for include in ['api.i', 'detail.i']:
            self.assertFalse(any(path.endswith('/' + include) for path in compiled['inputs']))
        if perl:
            self.assertIn('-DPERL_SDK', compiled['cmds'][0]['cmd_args'])
        if case == 'generated-source':
            copied = lib.node_by_output(graph, source)
            self.assertIn('$(S)/bindings/api.swg', copied['inputs'])
            self.assertIn(copied['uid'], node['deps'])
        output = {'py2module-version': 'libCedar.so', 'dll-prefix-first': '_Cedar.so',
                  'program-default': 'Cedar', 'library-unnamed': 'libbindings-python.a'}.get(case, 'libCedar.a')
        owner = lib.node_by_output(graph, '$(B)/bindings/python/' + output)
        expected_outputs = ['$(B)/bindings/python/' + output]
        if case == 'py2module-version':
            expected_outputs.append('$(B)/bindings/python/libCedar.so.2')
        self.assertEqual(owner['outputs'], expected_outputs + [module])
        self.assertIn(node['uid'], owner['deps'])
        self.assertEqual(owner['inputs'].count(wrapper), 1)
        self.assertNotIn(module, owner['inputs'])
        self.assertFalse([n for n in graph['graph'] if any(out.endswith(('.yapyc', '.reg3.cpp')) for out in n['outputs'])])

    def test_default_python_preserves_swig_and_cpp_include_roles(self):
        self.assert_recipe('library-default', *self.make('library-default'))

    def test_native_module_name_prefix_version_and_owner_outputs(self):
        for case in ['py2module-version', 'dll-prefix-first', 'program-default', 'library-unnamed']:
            with self.subTest(case=case):
                self.assert_recipe(case, *self.make(case))

    def test_relative_and_generated_sources_keep_producer_dependencies(self):
        for case in ['subdir-source', 'generated-source', 'perl-subdir']:
            with self.subTest(case=case):
                self.assert_recipe(case, *self.make(case))

    def test_language_and_implicit_peers_use_final_configuration(self):
        for case in ['explicit-python', 'late-python', 'cli-perl', 'late-perl']:
            with self.subTest(case=case):
                self.assert_recipe(case, *self.make(case))

    def test_explicit_empty_language_does_not_enable_default_python(self):
        for case in ['cli-empty', 'source-empty']:
            with self.subTest(case=case):
                result = lib.make_process(fixture(case), 'bindings/python', *flags(case))
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('SWIG_LANG', result.stderr)


if __name__ == '__main__':
    unittest.main(verbosity=2)
