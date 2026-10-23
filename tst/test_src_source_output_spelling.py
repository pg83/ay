import unittest

import lib


CASES={}
for macro in ('SRC','SRCS'):
 for kind,token,source in (
  ('own','local.cpp','meadow/core/check/local.cpp'),
  ('subdir','sub/local.cpp','meadow/core/check/sub/local.cpp'),
  ('parent','../parent.cpp','meadow/core/parent.cpp'),
  ('rooted','${ARCADIA_ROOT}/meadow/foreign/root.cpp','meadow/foreign/root.cpp'),
  ('fallback','shared.cpp','meadow/core/shared.cpp'),
  ('generated','generated.cpp','$(B)/meadow/core/check/generated.cpp'),
 ):
  CASES[macro.lower()+'-'+kind]=(macro,kind,token,source)
CASES['src-fallback-subdir']=('SRC','fallback','sub/shared.cpp','meadow/core/sub/shared.cpp')
CASES['src-generated-rooted']=('SRC','generated-rooted','${BINDIR}/stage/generated.cpp','$(B)/meadow/core/check/stage/generated.cpp')
CASES['unittest-src']=('SRC','unittest','shared.cpp','meadow/core/shared.cpp')
CASES['unittest-srcs']=('SRCS','unittest','shared.cpp','meadow/core/shared.cpp')
def fixture(name):
 macro,kind,token,source=CASES[name]
 bare='NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n'
 before='SRCDIR(meadow/core)\n' if kind=='fallback' else ''
 if kind=='generated':before='COPY_FILE(seed.cpp generated.cpp)\n'
 if kind=='generated-rooted':before='COPY_FILE(seed.cpp ${BINDIR}/stage/generated.cpp)\n'
 header='UNITTEST_FOR(meadow/core)' if kind=='unittest' else 'LIBRARY(bundle)'
 files={
  'meadow/core/check/ya.make':header+'\n'+bare+before+macro+'('+token+')\nEND()\n',
  'meadow/core/ya.make':'LIBRARY(core)\n'+bare+'END()\n',
  'library/cpp/testing/unittest_main/ya.make':'LIBRARY(test_main)\n'+bare+'END()\n',
  'meadow/core/check/seed.cpp':'#include "marker.h"\nint generated;\n',
  'meadow/core/check/marker.h':'#pragma once\n',
  'meadow/core/check/stage/marker.h':'#pragma once\n',
 }
 if not source.startswith('$(B)'):
  files[source]='#include "marker.h"\nint selected;\n'
  files[source.rsplit('/',1)[0]+'/marker.h']='#pragma once\n'
 return files


OUTPUTS = {
    'src-own': 'meadow/core/check/local.cpp.o',
    'src-subdir': 'meadow/core/check/sub/local.cpp.o',
    'src-parent': 'meadow/core/parent.cpp.o',
    'src-rooted': 'meadow/foreign/root.cpp.o',
    'src-fallback': 'meadow/core/check/shared.cpp.o',
    'src-generated': 'meadow/core/check/generated.cpp.o',
    'srcs-own': 'meadow/core/check/local.cpp.o',
    'srcs-subdir': 'meadow/core/check/_/sub/local.cpp.o',
    'srcs-parent': 'meadow/core/check/__/parent.cpp.o',
    'srcs-rooted': 'meadow/core/check/__/__/foreign/root.cpp.o',
    'srcs-fallback': 'meadow/core/check/__/shared.cpp.o',
    'srcs-generated': 'meadow/core/check/generated.cpp.o',
    'src-fallback-subdir': 'meadow/core/check/sub/shared.cpp.o',
    'src-generated-rooted': 'meadow/core/check/stage/generated.cpp.o',
    'unittest-src': 'meadow/core/check/shared.cpp.o',
    'unittest-srcs': 'meadow/core/check/__/shared.cpp.o',
}


class SrcSourceOutputSpellingTest(unittest.TestCase):
    def test_src_generator_keeps_its_own_generated_compile_output(self):
        files = {
            'meadow/gen/ya.make': 'LIBRARY()\n' + lib.BARE_MODULE + 'SRC(leaf.rl6)\nEND()\n',
            'meadow/gen/leaf.rl6': '%%{ machine leaf; }%%\n',
        }
        lib.tool_program(files, 'contrib/tools/ragel6', 'ragel6')
        graph = lib.make(files, 'meadow/gen')
        generated = lib.node_by_output(graph, '$(B)/meadow/gen/leaf.rl6.cpp')
        compiled = lib.node_by_output(graph, '$(B)/meadow/gen/leaf.rl6.cpp.o')
        self.assertIn('$(S)/meadow/gen/leaf.rl6', generated['inputs'])
        self.assertIn(generated['outputs'][0], compiled['inputs'])
        self.assertIn(generated['uid'], compiled['deps'])
        archive = next(n for n in graph['graph'] if n['kv'].get('p') == 'AR'
                       and compiled['outputs'][0] in n['inputs'])
        self.assertIn(compiled['uid'], archive['deps'])
        self.assertIn(archive['uid'], graph['result'])

    def test_explicit_source_output_does_not_inherit_resolved_source_directory(self):
        for kind, (_, _, _, source) in CASES.items():
            with self.subTest(kind=kind):
                graph = lib.make(fixture(kind), 'meadow/core/check', '--target-platform', 'default-linux-x86_64')
                compiled = lib.node_by_output(graph, '$(B)/' + OUTPUTS[kind])
                physical = source if source.startswith('$(B)') else '$(S)/' + source
                self.assertIn(physical, compiled['inputs'])
                self.assertEqual(compiled['inputs'].count(physical), 1)
                args = compiled['cmds'][0]['cmd_args']
                self.assertEqual(args[args.index('-o') + 1], compiled['outputs'][0])
                self.assertIn(physical, args)
                if not source.startswith('$(B)'):
                    self.assertTrue(any(value.endswith('/marker.h') for value in compiled['inputs']))
                consumer = next(node for node in graph['graph']
                                if node['kv'].get('p') in ('AR', 'LD') and compiled['outputs'][0] in node['inputs'])
                self.assertIn(compiled['uid'], consumer['deps'])
                self.assertTrue(any(compiled['outputs'][0] in cmd['cmd_args'] for cmd in consumer['cmds']))
                ids = {n['uid'] for n in graph['graph']}
                self.assertTrue(graph['result'])
                self.assertTrue(set(graph['result']) <= ids)
                self.assertTrue(all(set(n['deps']) <= ids for n in graph['graph']))
                nodes = {n['uid']: n for n in graph['graph']}
                pending = list(graph['result'])
                reachable = set()
                while pending:
                    uid = pending.pop()
                    if uid not in reachable:
                        reachable.add(uid)
                        pending.extend(nodes[uid]['deps'])
                self.assertIn(consumer['uid'], reachable)
                if source.startswith('$(B)'):
                    producer = lib.node_by_output(graph, source)
                    self.assertIn(producer['uid'], compiled['deps'])
                    self.assertIn('$(S)/meadow/core/check/seed.cpp', producer['inputs'])


if __name__ == '__main__':
    unittest.main()
