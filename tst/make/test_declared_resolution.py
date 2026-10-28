from tst.make.lib import MakeTest, PROJECT


class DeclaredResolutionTest(MakeTest):
    def test_module_then_srcdirs_then_source_root(self):
        self.write('ya.make', f'''LIBRARY()
SRCDIR({PROJECT}/first {PROJECT}/second)
SRCS(local.cpp shared.cpp fallback.cpp {PROJECT}/root/root.cpp)
END()
''')
        for name in ('local.cpp', 'first/local.cpp', 'first/shared.cpp',
                     'second/shared.cpp', 'second/fallback.cpp', 'root/root.cpp'):
            self.write(name, 'int value;\n')
        graph = self.graph()
        inputs = {path for node in graph['graph'] for path in node['inputs']}
        for name in ('local.cpp', 'first/shared.cpp', 'second/fallback.cpp', 'root/root.cpp'):
            self.assertIn(f'$(S)/{PROJECT}/{name}', inputs)
        for name in ('first/local.cpp', 'second/shared.cpp'):
            self.assertNotIn(f'$(S)/{PROJECT}/{name}', inputs)

    def test_forward_generated_and_rooted_sources(self):
        self.write('ya.make', f'''LIBRARY()
SRCS(generated.cpp ${{ARCADIA_ROOT}}/{PROJECT}/root.cpp ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/explicit.cpp)
RUN_PYTHON3(generate.py OUT_NOAUTO generated.cpp explicit.cpp)
END()
''')
        self.write('generate.py', 'print("generated")\n')
        self.write('root.cpp', 'int root;\n')
        graph = self.graph()
        for name in ('generated.cpp', 'explicit.cpp'):
            self.assertIn(f'$(B)/{PROJECT}/{name}', self.node(graph, name + '.o')['inputs'])
        self.assertIn(f'$(S)/{PROJECT}/root.cpp', self.node(graph, 'root.cpp.o')['inputs'])

    def test_python_srcdir_and_template_names(self):
        self.write('ya.make', f'''PY3_LIBRARY()
SRCDIR({PROJECT}/first {PROJECT}/second)
PY_SRCS(local.py shared.py)
SRCS(config.cpp.in)
END()
''')
        for name in ('local.py', 'first/local.py', 'first/shared.py', 'second/shared.py'):
            self.write(name, 'value = 42\n')
        self.write('first/config.cpp.in', 'int configured;\n')
        graph = self.graph()
        inputs = {path for node in graph['graph'] for path in node['inputs']}
        self.assertIn(f'$(S)/{PROJECT}/local.py', inputs)
        self.assertIn(f'$(S)/{PROJECT}/first/shared.py', inputs)
        self.assertNotIn(f'$(S)/{PROJECT}/first/local.py', inputs)
        self.assertNotIn(f'$(S)/{PROJECT}/second/shared.py', inputs)
        template = next(node for node in graph['graph']
                        if f'$(S)/{PROJECT}/first/config.cpp.in' in node['inputs']
                        and any(out.endswith('/config.cpp') for out in node['outputs']))
        self.assertTrue(any(template['outputs'][0] in node['inputs']
                            and any(out.endswith('/config.cpp.o') for out in node['outputs'])
                            for node in graph['graph']))

    def test_generated_srcdir_is_bound_when_producer_is_submitted(self):
        self.write('ya.make', f'''LIBRARY()
RUN_PYTHON3(generate.py OUT_NOAUTO generated/value.cpp)
SRCDIR(${{ARCADIA_BUILD_ROOT}}/{PROJECT}/generated)
COPY_FILE(value.cpp copied.cpp)
SRCS(copied.cpp)
END()
''')
        self.write('generate.py', 'print("generated")\n')
        graph = self.graph()
        self.assertIn(f'$(B)/{PROJECT}/generated/value.cpp', self.node(graph, 'copied.cpp')['inputs'])

    def test_python_generated_srcdir_is_bound_when_producer_is_submitted(self):
        self.write('ya.make', f'''PY3_LIBRARY()
RUN_PYTHON3(generate.py OUT_NOAUTO generated/value.py)
SRCDIR(${{ARCADIA_BUILD_ROOT}}/{PROJECT}/generated)
PY_SRCS(value.py)
END()
''')
        self.write('generate.py', 'print("generated")\n')
        graph = self.graph()
        producer = self.node(graph, 'generated/value.py')
        consumer = self.node(graph, 'value.py.yapyc3')
        self.assertIn(f'$(B)/{PROJECT}/generated/value.py', consumer['inputs'])
        self.assertIn(producer['uid'], consumer['deps'])

    def test_python_generated_srcdir_in_shared_peer(self):
        self.write('ya.make', 'RECURSE(first second)\n')
        for name in ('first', 'second'):
            self.write(f'{name}/ya.make', f'''PY3_PROGRAM()
PEERDIR({PROJECT}/generated)
PY_SRCS(__main__.py)
END()
''')
            self.write(f'{name}/__main__.py', 'import value\n')
        self.write('generated/ya.make', f'''PY3_LIBRARY()
PY_NAMESPACE(.)
PY_SRCS(value.py)
USRV_BUILD(FROM sources.tar {PROJECT}/generated/value.py)
SRCDIR(${{CMAKE_CURRENT_BINARY_DIR}}/{PROJECT}/generated)
END()
''')
        self.write('generated/sources.tar', '')
        graph = self.graph()
        output = f'generated/{PROJECT}/generated/value.py'
        producer = self.node(graph, output)
        consumer = self.node(graph, 'generated/value.py.yapyc3')
        self.assertEqual(producer['kv']['p'], 'UT')
        self.assertIn(f'$(B)/{PROJECT}/{output}', consumer['inputs'])
        self.assertIn(producer['uid'], consumer['deps'])
