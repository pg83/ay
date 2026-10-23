"""Native header configuration is distinct from Python runtime compatibility."""
import unittest

import lib
from test_proto_py2_context import proto_world
from test_python_proto import args


HEADERS = 'renamed/extension'
SCHEMA = 'renamed/messages'
INCLUDE = '-I$(S)/contrib/libs/python/Include'
UNICODE = '-DARCADIA_PYTHON_UNICODE_SIZE=4'


def world(macro='PYTHON2_ADDINCL()', settings='', declaration='LIBRARY(extension)', policy=''):
    files = proto_world()
    for path in list(files):
        if path.split('/')[0] in ('p', 'q', 'py'):
            del files[path]
    files.update({
        HEADERS + '/ya.make': (
            f'{declaration}\n{lib.BARE_MODULE}{settings}\n'
            'INCLUDE(${ARCADIA_ROOT}/shared/headers.inc)\n'
            f'{policy}\nPY_REGISTER(native_module)\nPEERDIR({SCHEMA} renamed/plain)\nSRCS(native.cpp)\nEND()\n'
        ),
        'shared/headers.inc': macro + '\n',
        HEADERS + '/native.cpp': '#include <Python.h>\nint extension;\n',
        SCHEMA + '/ya.make': f'PROTO_SCHEMA(messages)\n{lib.BARE_MODULE}PEERDIR(renamed/common)\nSRCS(record.proto)\nEND()\n',
        SCHEMA + '/record.proto': 'syntax = "proto2";\nimport "renamed/common/base.proto";\nmessage Record {}\n',
        'renamed/common/ya.make': f'PROTO_LIBRARY(common)\n{lib.BARE_MODULE}SRCS(base.proto)\nEND()\n',
        'renamed/common/base.proto': 'syntax = "proto2";\nmessage Base {}\n',
        'renamed/plain/ya.make': f'LIBRARY(plain)\n{lib.BARE_MODULE}CFLAGS(GLOBAL -DPLAIN_PEER)\nSRCS(plain.cpp)\nEND()\n',
        'renamed/plain/plain.cpp': 'int plain;\n',
        'renamed/consumer/ya.make': f'PROGRAM(reader)\n{lib.BARE_MODULE}PEERDIR({HEADERS})\nSRCS(reader.cpp)\nEND()\n',
        'renamed/consumer/reader.cpp': '#include <Python.h>\nint reader;\n',
        'renamed/python/ya.make': f'PY2_PROGRAM(reader)\n{lib.BARE_MODULE}PEERDIR({SCHEMA})\nPY_SRCS(reader.py)\nEND()\n',
        'renamed/python/reader.py': 'value = 1\n',
        'build/scripts/gen_py_reg.py': '# graph-only tool input\n',
        'build/scripts/gen_py3_reg.py': '# graph-only tool input\n',
        'build/platform/python/ya.make': f'LIBRARY(system)\n{lib.BARE_MODULE}ADDINCL(GLOBAL supplied)\nCFLAGS(GLOBAL -DSYSTEM_HEADERS)\nSRCS(system.cpp)\nEND()\n',
        'build/platform/python/system.cpp': 'int system;\n',
        'supplied/Python.h': '#pragma once\n',
    })
    files['contrib/libs/python/ya.make'] = files['contrib/libs/python/ya.make'].replace('PY23_LIBRARY()', 'PY23_LIBRARY(python)').replace('END()', 'SRCS(native_runtime.cpp)\nEND()')
    files['contrib/libs/python/native_runtime.cpp'] = 'int runtime;\n'
    return files


def make(files, roots):
    return lib.make(files, roots[-1], *roots[:-1])


def outputs(graph):
    return {out for node in graph['graph'] for out in node['outputs']}


class Python2NativeHeadersTest(unittest.TestCase):
    def assert_complete(self, graph):
        ids = {node['uid'] for node in graph['graph']}
        self.assertTrue(set(graph['result']).issubset(ids))
        for node in graph['graph']:
            self.assertTrue(set(node['deps']).issubset(ids), node['outputs'])

    def registration(self, graph, version=2, suffix=''):
        path = f'$(B)/{HEADERS}/native_module.reg' + ('3' if version == 3 else '') + '.cpp'
        producer = lib.node_by_output(graph, path)
        compiled = lib.node_by_output(graph, path + suffix + '.o')
        self.assertIn(producer['uid'], compiled['deps'])
        self.assertIn(path, compiled['inputs'])
        self.assertIn(f'$(S)/build/scripts/gen_py{"3" if version == 3 else ""}_reg.py', producer['inputs'])
        self.assertEqual(args(producer)[-1], path)
        archive = lib.node_by_output(graph, f'$(B)/{HEADERS}/libextension.global.a')
        self.assertIn(compiled['uid'], archive['deps'])
        self.assertIn(compiled['outputs'][0], archive['inputs'])
        return compiled

    def test_native_headers_keep_real_header_and_registration_without_runtime_or_python_proto(self):
        files = world(settings=(
            'SET(ARCADIA_PYTHON_UNICODE_SIZE 4)\n'
            'SET(YQL_UDF_STATIC_PEERDIR_TAG STATIC_VARIANT)\nSET(YQL_ABI_PEERDIR_TAG ABI_VARIANT)'))
        files['shared/headers.inc'] += (
            'IF (PYTHON2 == "yes" AND PYTHON3 == "no" AND _PYTHON_VER == "py2" '
            'AND PYTHON_TYPE_FOR_CYTHON == "PY2" AND MODULE_TAG == "PY2_NATIVE" '
            'AND PEERDIR_TAGS == "CPP_PROTO CPP_PROTO_DIRECT_HEADERS CPP_PROTO_LEGACY_FOR_DIRECT_HEADERS CPP_PROTO_FROM_SCHEMA CPP_FBS CPP_ROS PY2_NATIVE STATIC_VARIANT ABI_VARIANT __EMPTY__ RESOURCE_LIB DLL_LIB")\n'
            'CFLAGS(-DHEADER_ENV)\nENDIF()\n')
        files[HEADERS + '/native.cpp'] += '#include <renamed/messages/record.pb.h>\n'
        graph = make(files, ['renamed/consumer'])
        self.assert_complete(graph)
        own = lib.node_by_output(graph, f'$(B)/{HEADERS}/native.cpp.o')
        proto = lib.node_by_output(graph, f'$(B)/{SCHEMA}/record.pb.h')
        self.assertIn(proto['uid'], own['deps'])
        self.assertIn(f'$(B)/{SCHEMA}/record.pb.h', own['inputs'])
        self.assertIn(f'$(S)/{SCHEMA}/record.proto', proto['inputs'])
        self.assertIn('$(S)/renamed/common/base.proto', proto['inputs'])
        self.assertIn('$(B)/contrib/tools/protoc/protoc', args(proto))
        registered = self.registration(graph)
        for node in (own, registered):
            self.assertEqual(args(node).count(INCLUDE), 1)
            self.assertEqual(args(node).count(UNICODE), 1)
            self.assertIn('-DHEADER_ENV', args(node))
            self.assertIn('-DPLAIN_PEER', args(node))
            self.assertNotIn('-DUSE_PYTHON2', args(node))
        self.assertIn('$(S)/contrib/libs/python/Include/Python.h', own['inputs'])
        self.assertIn(f'$(S)/{HEADERS}/native.cpp', own['inputs'])
        downstream = lib.node_by_output(graph, '$(B)/renamed/consumer/reader.cpp.o')
        self.assertIn(INCLUDE, args(downstream))
        self.assertIn('$(S)/contrib/libs/python/Include/Python.h', downstream['inputs'])
        self.assertNotIn(UNICODE, args(downstream))
        lib.node_by_output(graph, f'$(B)/{SCHEMA}/record.pb.h')
        self.assertFalse(any('__intpy' in out for out in outputs(graph)))
        self.assertFalse(any('/library/python/runtime/' in out or '/contrib/libs/python/lib' in out for out in outputs(graph)))

    def test_non_arcadia_system_and_supplied_flags_follow_separate_branches(self):
        for system in (True, False):
            with self.subTest(system=system):
                settings = 'SET(USE_ARCADIA_PYTHON no)\n' + (
                    'SET(USE_SYSTEM_PYTHON 2.7)' if system else
                    'SET(HEADER_FLAGS -Isupplied -DSUPPLIED_HEADERS)\nSET(PYTHON_INCLUDE ${HEADER_FLAGS})')
                files = world(settings=settings)
                files[HEADERS + '/native.cpp'] = '#include <Python.h>\n' if system else 'int extension;\n'
                files['renamed/consumer/reader.cpp'] = '#include <Python.h>\n' if system else 'int reader;\n'
                graph = make(files, ['renamed/consumer'])
                self.assert_complete(graph)
                own = lib.node_by_output(graph, f'$(B)/{HEADERS}/native.cpp.o')
                self.registration(graph)
                self.assertNotIn(INCLUDE, args(own))
                self.assertNotIn(UNICODE, args(own))
                self.assertNotIn('-DUSE_PYTHON2', args(own))
                if system:
                    self.assertIn('-DSYSTEM_HEADERS', args(own))
                    self.assertIn('$(S)/supplied/Python.h', own['inputs'])
                    lib.node_by_output(graph, '$(B)/build/platform/python/libsystem.a')
                else:
                    self.assertIn('-Isupplied', args(own))
                    self.assertIn('-DSUPPLIED_HEADERS', args(own))
                    self.assertNotIn('$(B)/build/platform/python/libsystem.a', outputs(graph))
                self.assertFalse(any('__intpy' in out for out in outputs(graph)))

    def test_system_header_condition_tests_nonempty_value_in_both_versions(self):
        for macro in ('PYTHON2_ADDINCL()', 'PYTHON3_ADDINCL()'):
            for value in ('no', 'false', '0'):
                with self.subTest(macro=macro, value=value):
                    files = world(macro, settings=f'SET(USE_ARCADIA_PYTHON no)\nSET(USE_SYSTEM_PYTHON {value})\nSET(PYTHON_INCLUDE -DUNUSED_FALLBACK)')
                    files[HEADERS + '/native.cpp'] = 'int extension;\n'
                    files['renamed/consumer/reader.cpp'] = 'int reader;\n'
                    graph = make(files, ['renamed/consumer'])
                    self.assert_complete(graph)
                    own = lib.node_by_output(graph, f'$(B)/{HEADERS}/native.cpp.o')
                    self.assertIn('-DSYSTEM_HEADERS', args(own))
                    self.assertNotIn('-DUNUSED_FALLBACK', args(own))
                    self.assertNotIn(INCLUDE, args(own))
                    self.assertNotIn(UNICODE, args(own))
                    self.assertIn('-I$(S)/supplied', args(own))
                    lib.node_by_output(graph, '$(B)/build/platform/python/libsystem.a')

    def test_use_python2_keeps_runtime_but_uses_final_cpp_compatible_schema_policy(self):
        files = world('USE_PYTHON2()')
        graph = make(files, ['renamed/consumer'])
        self.assert_complete(graph)
        own = lib.node_by_output(graph, f'$(B)/{HEADERS}/native.cpp.o')
        self.assertIn('-DUSE_PYTHON2', args(own))
        self.assertIn(INCLUDE, args(own))
        self.assertIn(UNICODE, args(own))
        lib.node_by_output(graph, '$(B)/contrib/libs/python/libpypython.a')
        lib.node_by_output(graph, f'$(B)/{SCHEMA}/record.pb.h')
        self.assertFalse(any('__intpy' in out for out in outputs(graph)))
        self.registration(graph)

    def test_shared_include_and_root_orders_preserve_distinct_native_and_python_contexts(self):
        files = world()
        files['renamed/other/ya.make'] = (f'LIBRARY(other)\n{lib.BARE_MODULE}'
            'SET(USE_ARCADIA_PYTHON no)\nSET(PYTHON_INCLUDE -DSUPPLIED_OTHER)\n'
            'INCLUDE(${ARCADIA_ROOT}/shared/headers.inc)\nSRCS(other.cpp)\nEND()\n')
        files['renamed/other/other.cpp'] = 'int other;\n'
        semantic = []
        for roots in (['renamed/consumer', 'renamed/python', 'renamed/other'], ['renamed/other', 'renamed/python', 'renamed/consumer']):
            graph = make(files, roots)
            self.assert_complete(graph)
            native = lib.node_by_output(graph, f'$(B)/{HEADERS}/native.cpp.o')
            self.assertEqual(args(native).count(INCLUDE), 1)
            self.assertNotIn('-DUSE_PYTHON2', args(native))
            self.assertNotIn('-DSUPPLIED_OTHER', args(native))
            other = lib.node_by_output(graph, '$(B)/renamed/other/other.cpp.o')
            self.assertIn('-DSUPPLIED_OTHER', args(other))
            self.assertNotIn(INCLUDE, args(other))
            producer = lib.node_by_output(graph, f'$(B)/{SCHEMA}/record__intpy2___pb2.py')
            self.assertIn('$(B)/contrib/tools/protoc_old/protoc_old', args(producer))
            bytecode = next(n for n in graph['graph'] if any('record__intpy2___pb2.py.' in out and out.endswith('.yapyc') for out in n['outputs']))
            self.assertIn(producer['uid'], bytecode['deps'])
            resource = next(n for n in graph['graph'] if any('resfs/src//py_modules/' in x and 'record__intpy2___pb2.py' in x for x in args(n)))
            self.assertIn(producer['uid'], resource['deps'])
            self.registration(graph)
            semantic.append(sorted((n.get('platform', ''), n['outputs'], n['inputs'], str(n['cmds'])) for n in graph['graph']))
        self.assertEqual(semantic[0], semantic[1])

    def test_final_declared_peer_tags_select_actual_python_proto_contexts(self):
        for version, tag in ((2, 'PY_PROTO_FROM_SCHEMA'), (3, 'PY3_PROTO_FROM_SCHEMA')):
            with self.subTest(version=version):
                graph = make(world(policy=f'SET(PEERDIR_TAGS {tag} __EMPTY__)'), ['renamed/consumer'])
                self.assert_complete(graph)
                lib.node_by_output(graph, f'$(B)/{SCHEMA}/record__intpy{version}___pb2.py')
                other = 3 if version == 2 else 2
                self.assertFalse(any(f'__intpy{other}__' in out for out in outputs(graph)))

    def test_ordinary_proto_library_uses_cpp_tag_in_both_native_versions(self):
        for version in (2, 3):
            with self.subTest(version=version):
                files = world(f'PYTHON{version}_ADDINCL()')
                files[SCHEMA + '/ya.make'] = files[SCHEMA + '/ya.make'].replace('PROTO_SCHEMA', 'PROTO_LIBRARY')
                graph = make(files, ['renamed/consumer'])
                self.assert_complete(graph)
                own = lib.node_by_output(graph, f'$(B)/{HEADERS}/native.cpp.o')
                marker = '-DUSE_PYTHON3' if version == 3 else UNICODE
                self.assertIn(marker, args(own))
                self.registration(graph, version)
                lib.node_by_output(graph, f'$(B)/{SCHEMA}/record.pb.h')
                self.assertFalse(any('__intpy' in out for out in outputs(graph)))

    def test_explicit_cpp_only_schema_retains_native_headers_and_runtime_choice(self):
        for macro in ('PYTHON2_ADDINCL()', 'USE_PYTHON2()'):
            with self.subTest(macro=macro):
                files = world(macro)
                files[SCHEMA + '/ya.make'] = files[SCHEMA + '/ya.make'].replace('SRCS(record.proto)', 'ONLY_TAGS(CPP_PROTO_FROM_SCHEMA)\nSRCS(record.proto)')
                graph = make(files, ['renamed/consumer'])
                self.assert_complete(graph)
                own = lib.node_by_output(graph, f'$(B)/{HEADERS}/native.cpp.o')
                self.assertIn(UNICODE, args(own))
                self.assertEqual('-DUSE_PYTHON2' in args(own), macro == 'USE_PYTHON2()')
                lib.node_by_output(graph, f'$(B)/{SCHEMA}/record.pb.h')
                self.assertFalse(any('__intpy' in out for out in outputs(graph)))

    def test_native_module_inherited_header_setup_in_selected_python2_and_python3_contexts(self):
        files = world('', declaration='PY23_NATIVE_LIBRARY(extension)')
        files['renamed/python/ya.make'] = f'PY2_PROGRAM(reader)\n{lib.BARE_MODULE}PEERDIR({HEADERS})\nEND()\n'
        files['renamed/python3/ya.make'] = f'PY3_PROGRAM(reader3)\n{lib.BARE_MODULE}PEERDIR({HEADERS})\nEND()\n'
        for roots in (['renamed/python', 'renamed/python3'], ['renamed/python3', 'renamed/python']):
            graph = make(files, roots)
            self.assert_complete(graph)
            for suffix, marker in (('.py2', UNICODE), ('.py3', '-DUSE_PYTHON3')):
                own = lib.node_by_output(graph, f'$(B)/{HEADERS}/native.cpp{suffix}.o')
                self.assertIn(marker, args(own))
                self.assertEqual(args(own).count(INCLUDE), 1)
                self.assertIn('$(S)/contrib/libs/python/Include/Python.h', own['inputs'])
            self.assertFalse(any('__intpy' in out for out in outputs(graph)))

    def test_repeated_calls_and_late_configuration_apply_header_fields_once(self):
        for version in (2, 3):
            macro = f'PYTHON{version}_ADDINCL()'
            with self.subTest(version=version, branch='repeat'):
                files = world(macro + '\n' + macro, settings='SET(CFLAGS)')
                marker = UNICODE if version == 2 else '-DUSE_PYTHON3'
                files['shared/headers.inc'] += f'IF (CFLAGS == "{marker}")\nCFLAGS(-DFIELDS_VISIBLE_TOO_EARLY)\nENDIF()\n'
                graph = make(files, ['renamed/consumer'])
                self.assert_complete(graph)
                own = lib.node_by_output(graph, f'$(B)/{HEADERS}/native.cpp.o')
                self.assertEqual(args(own).count(marker), 1)
                self.assertEqual(args(own).count(INCLUDE), 1)
                self.assertNotIn('-DFIELDS_VISIBLE_TOO_EARLY', args(own))
            with self.subTest(version=version, branch='late-system'):
                files = world(macro, policy='SET(USE_ARCADIA_PYTHON no)\nSET(USE_SYSTEM_PYTHON 2.7)')
                graph = make(files, ['renamed/consumer'])
                self.assert_complete(graph)
                own = lib.node_by_output(graph, f'$(B)/{HEADERS}/native.cpp.o')
                self.assertIn('-DSYSTEM_HEADERS', args(own))
                self.assertNotIn(INCLUDE, args(own))
                self.assertIn('$(S)/supplied/Python.h', own['inputs'])
        files = world(policy='SET(ARCADIA_PYTHON_UNICODE_SIZE 9)')
        graph = make(files, ['renamed/consumer'])
        self.assertIn('-DARCADIA_PYTHON_UNICODE_SIZE=9', args(lib.node_by_output(graph, f'$(B)/{HEADERS}/native.cpp.o')))

    def test_native_python3_context_aliases_python2_header_declaration(self):
        files = world(declaration='PY23_NATIVE_LIBRARY(extension)')
        files['renamed/python/ya.make'] = f'PY2_PROGRAM(reader)\n{lib.BARE_MODULE}PEERDIR({HEADERS})\nEND()\n'
        files['renamed/python3/ya.make'] = f'PY3_PROGRAM(reader3)\n{lib.BARE_MODULE}PEERDIR({HEADERS})\nEND()\n'
        for roots in (['renamed/python', 'renamed/python3'], ['renamed/python3', 'renamed/python']):
            graph = make(files, roots)
            self.assert_complete(graph)
            for suffix, marker in (('.py2', UNICODE), ('.py3', '-DUSE_PYTHON3')):
                own = lib.node_by_output(graph, f'$(B)/{HEADERS}/native.cpp{suffix}.o')
                self.assertEqual(args(own).count(marker), 1)
                self.assertEqual(args(own).count(INCLUDE), 1)
            self.assertFalse(any('__intpy' in out for out in outputs(graph)))

    def test_peer_candidates_skip_excluded_contexts_in_native_config_order(self):
        for declaration, cpp, py2, py3 in (('PROTO_SCHEMA', 'CPP_PROTO_FROM_SCHEMA', 'PY_PROTO_FROM_SCHEMA', 'PY3_PROTO_FROM_SCHEMA'),
                                           ('PROTO_LIBRARY', 'CPP_PROTO', 'PY_PROTO', 'PY3_PROTO')):
            for excluded, selected in (((cpp,), 3), ((cpp, py3), 2)):
                with self.subTest(declaration=declaration, excluded=excluded):
                    policy = f'SET(PEERDIR_TAGS {cpp} {py2} {py3} __EMPTY__)'
                    files = world('USE_PYTHON2()', policy=policy)
                    files[SCHEMA + '/ya.make'] = files[SCHEMA + '/ya.make'].replace('PROTO_SCHEMA', declaration).replace('SRCS(record.proto)', 'EXCLUDE_TAGS(' + ' '.join(excluded) + ')\nSRCS(record.proto)')
                    graph = make(files, ['renamed/consumer'])
                    self.assert_complete(graph)
                    lib.node_by_output(graph, f'$(B)/{SCHEMA}/record__intpy{selected}___pb2.py')
                    self.assertFalse(any(f'$(B)/{SCHEMA}/record__intpy{5-selected}__' in out for out in outputs(graph)))
                    self.assertNotIn(f'$(B)/{SCHEMA}/record.pb.h', outputs(graph))
                    self.assertIn('-DUSE_PYTHON2', args(lib.node_by_output(graph, f'$(B)/{HEADERS}/native.cpp.o')))

    def test_cpp_proto_root_and_header_consumer_share_one_selected_context(self):
        files = world()
        files[SCHEMA + '/ya.make'] = files[SCHEMA + '/ya.make'].replace('PROTO_SCHEMA', 'PROTO_LIBRARY').replace('SRCS(record.proto)', 'ONLY_TAGS(CPP_PROTO)\nSRCS(record.proto)')
        for roots in ([SCHEMA, 'renamed/consumer'], ['renamed/consumer', SCHEMA], [SCHEMA, HEADERS], [HEADERS, SCHEMA]):
            graph = make(files, roots)
            self.assert_complete(graph)
            self.assertEqual(sum(f'$(B)/{SCHEMA}/record.pb.h' in n['outputs'] for n in graph['graph']), 1)
            self.assertFalse(any('__intpy' in out for out in outputs(graph)))

    def test_excluded_only_compatible_candidate_is_rejected(self):
        for macro in ('PYTHON2_ADDINCL()', 'USE_PYTHON2()'):
            files = world(macro)
            files[SCHEMA + '/ya.make'] = files[SCHEMA + '/ya.make'].replace('SRCS(record.proto)', 'EXCLUDE_TAGS(CPP_PROTO_FROM_SCHEMA)\nSRCS(record.proto)')
            result = lib.make_process(files, 'renamed/consumer')
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('no compatible Python header peer', result.stderr)

    def test_python3_header_alias_binding_uses_final_fields_without_changing_source_assignment(self):
        for alias, directory in (('$HEADER_DIR', 'later'), ('${HEADER_DIR}', 'sdk')):
            with self.subTest(alias=alias):
                files = world('PYTHON3_ADDINCL()', settings=f'SET(HEADER_DIR renamed/sdk)\nSET(PY3_BASE_INCLUDE_DIR {alias})', policy='SET(HEADER_DIR renamed/later)')
                for path in ('sdk', 'later'):
                    files[f'renamed/{path}/Python.h'] = '#pragma once\n'
                graph = make(files, ['renamed/consumer'])
                self.assert_complete(graph)
                for path in (f'$(B)/{HEADERS}/native.cpp.o', '$(B)/renamed/consumer/reader.cpp.o'):
                    node = lib.node_by_output(graph, path)
                    self.assertIn(f'$(S)/renamed/{directory}/Python.h', node['inputs'])
                    self.assertIn(f'-I$(S)/renamed/{directory}', args(node))

    def test_declared_header_includes_precede_conditional_header_fields(self):
        files = world(settings='ADDINCL(GLOBAL renamed/before)', policy='ADDINCL(GLOBAL renamed/after)')
        for path in ('before', 'after'):
            files[f'renamed/{path}/Python.h'] = '#pragma once\n'
        graph = make(files, ['renamed/consumer'])
        self.assert_complete(graph)
        own = lib.node_by_output(graph, f'$(B)/{HEADERS}/native.cpp.o')
        command = args(own)
        self.assertLess(command.index('-I$(S)/renamed/before'), command.index('-I$(S)/renamed/after'))
        self.assertLess(command.index('-I$(S)/renamed/after'), command.index(INCLUDE))
        self.assertIn('$(S)/renamed/before/Python.h', own['inputs'])
        self.assertNotIn('$(S)/contrib/libs/python/Include/Python.h', own['inputs'])

    def test_incompatible_declared_proto_peer_policy_is_rejected(self):
        result = lib.make_process(world(policy='SET(PEERDIR_TAGS RESOURCE_LIB)'), HEADERS)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('no compatible Python header peer', result.stderr)


if __name__ == '__main__':
    unittest.main(verbosity=2)
