"""Source declarations and ordered native graph members; T117 pinned evidence.

Host tool groups use the separate native PIC host configuration, as in the
frontend host/target graph composition. Full graphs/configurations are retained.
"""

CASES = [{'name': 'library-physical-resource-before',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RESOURCE(- marker=1)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_f3a8c5b5e22c59124defd9adc1.o',
                          '$(B)/grove/ledger/stone.cpp.o']},
             {'output': '$(B)/grove/ledger/libledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'library-physical-resource-after',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'RESOURCE(- marker=1)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_f3a8c5b5e22c59124defd9adc1.o',
                          '$(B)/grove/ledger/stone.cpp.o']},
             {'output': '$(B)/grove/ledger/libledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'library-physical-python-before',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'PY_SRCS(note.py)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          '$(B)/grove/ledger/stone.cpp.o']},
             {'output': '$(B)/grove/ledger/libledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'library-physical-python-after',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'library-physical-sandwich',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RESOURCE(- marker=1)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'RESOURCE(- tail=1)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_2b460f8205f174fff41e6103ee.o',
                          '$(B)/grove/ledger/objcopy_2b023d41aaf5c744a58ad08180.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'library-generated-explicit',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.cpp)\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/formed.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']}]},
 {'name': 'library-generated-later',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.cpp)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/formed.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']}]},
 {'name': 'library-physical-resource-before-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /marker)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/b77e8c5bc0bfaee0db4b7b3bb4_raw.auxcpp.o',
                          '$(B)/grove/ledger/stone.cpp.o']},
             {'output': '$(B)/grove/ledger/libledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'library-physical-resource-after-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /marker)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/b77e8c5bc0bfaee0db4b7b3bb4_raw.auxcpp.o',
                          '$(B)/grove/ledger/stone.cpp.o']},
             {'output': '$(B)/grove/ledger/libledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'library-physical-python-before-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'PY_SRCS(conftest.py)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_4cccb6d65bbce96629ca3408b2.o',
                          '$(B)/grove/ledger/8d7321c2beeb41e9ca90318fd6_raw.auxcpp.o',
                          '$(B)/grove/ledger/stone.cpp.o']},
             {'output': '$(B)/grove/ledger/libledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'library-physical-python-after-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(conftest.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_4cccb6d65bbce96629ca3408b2.o',
                          '$(B)/grove/ledger/8d7321c2beeb41e9ca90318fd6_raw.auxcpp.o']},
             {'output': '$(B)/grove/ledger/libledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'library-physical-sandwich-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /marker)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(conftest.py)\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /tail)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/fa6d3163fb794c8e7a17397ac8_raw.auxcpp.o',
                          '$(B)/grove/ledger/c0d2d593f9fb9c123547691141_raw.auxcpp.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_4cccb6d65bbce96629ca3408b2.o',
                          '$(B)/grove/ledger/8d7321c2beeb41e9ca90318fd6_raw.auxcpp.o']},
             {'output': '$(B)/grove/ledger/libledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'library-generated-explicit-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.cpp)\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(conftest.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/formed.cpp.o',
                          '$(B)/grove/ledger/objcopy_4cccb6d65bbce96629ca3408b2.o',
                          '$(B)/grove/ledger/8d7321c2beeb41e9ca90318fd6_raw.auxcpp.o']}]},
 {'name': 'library-generated-later-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(conftest.py)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.cpp)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/formed.cpp.o',
                          '$(B)/grove/ledger/objcopy_4cccb6d65bbce96629ca3408b2.o',
                          '$(B)/grove/ledger/8d7321c2beeb41e9ca90318fd6_raw.auxcpp.o']}]},
 {'name': 'py3_library-physical-resource-before',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(- marker=1)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_2b460f8205f174fff41e6103ee.o',
                          '$(B)/grove/ledger/stone.cpp.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-physical-resource-after',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'RESOURCE(- marker=1)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_2b460f8205f174fff41e6103ee.o',
                          '$(B)/grove/ledger/stone.cpp.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-physical-python-before',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'PY_SRCS(note.py)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          '$(B)/grove/ledger/stone.cpp.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-physical-python-after',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-physical-sandwich',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(- marker=1)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'RESOURCE(- tail=1)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_2b460f8205f174fff41e6103ee.o',
                          '$(B)/grove/ledger/objcopy_2b023d41aaf5c744a58ad08180.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-generated-explicit',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.cpp)\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/formed.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']}]},
 {'name': 'py3_library-generated-later',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.cpp)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/formed.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']}]},
 {'name': 'py3_library-physical-resource-before-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /marker)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/fa6d3163fb794c8e7a17397ac8_raw.auxcpp.o',
                          '$(B)/grove/ledger/stone.cpp.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-physical-resource-after-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /marker)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/fa6d3163fb794c8e7a17397ac8_raw.auxcpp.o',
                          '$(B)/grove/ledger/stone.cpp.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-physical-python-before-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'PY_SRCS(conftest.py)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_4cccb6d65bbce96629ca3408b2.o',
                          '$(B)/grove/ledger/8d7321c2beeb41e9ca90318fd6_raw.auxcpp.o',
                          '$(B)/grove/ledger/stone.cpp.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-physical-python-after-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(conftest.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_4cccb6d65bbce96629ca3408b2.o',
                          '$(B)/grove/ledger/8d7321c2beeb41e9ca90318fd6_raw.auxcpp.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-physical-sandwich-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /marker)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(conftest.py)\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /tail)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/fa6d3163fb794c8e7a17397ac8_raw.auxcpp.o',
                          '$(B)/grove/ledger/c0d2d593f9fb9c123547691141_raw.auxcpp.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_4cccb6d65bbce96629ca3408b2.o',
                          '$(B)/grove/ledger/8d7321c2beeb41e9ca90318fd6_raw.auxcpp.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-generated-explicit-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.cpp)\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(conftest.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/formed.cpp.o',
                          '$(B)/grove/ledger/objcopy_4cccb6d65bbce96629ca3408b2.o',
                          '$(B)/grove/ledger/8d7321c2beeb41e9ca90318fd6_raw.auxcpp.o']}]},
 {'name': 'py3_library-generated-later-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(conftest.py)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.cpp)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/formed.cpp.o',
                          '$(B)/grove/ledger/objcopy_4cccb6d65bbce96629ca3408b2.o',
                          '$(B)/grove/ledger/8d7321c2beeb41e9ca90318fd6_raw.auxcpp.o']}]},
 {'name': 'py3_program_bin-physical-resource-before',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(- marker=1)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/objcopy_2b460f8205f174fff41e6103ee.o',
                          'grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_program_bin-physical-resource-after',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'RESOURCE(- marker=1)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/objcopy_2b460f8205f174fff41e6103ee.o',
                          'grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_program_bin-physical-python-before',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'PY_SRCS(note.py)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          'grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          'grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_program_bin-physical-python-after',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/stone.cpp.o',
                          'grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          'grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_program_bin-physical-sandwich',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(- marker=1)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'RESOURCE(- tail=1)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/objcopy_2b460f8205f174fff41e6103ee.o',
                          'grove/ledger/objcopy_2b023d41aaf5c744a58ad08180.o',
                          'grove/ledger/stone.cpp.o',
                          'grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          'grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_program_bin-generated-explicit',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.cpp)\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/formed.cpp.o',
                          'grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          'grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']}]},
 {'name': 'py3_program_bin-generated-later',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.cpp)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/formed.cpp.o',
                          'grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          'grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']}]},
 {'name': 'py3_program_bin-physical-resource-before-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /marker)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/fa6d3163fb794c8e7a17397ac8_raw.auxcpp.o',
                          'grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_program_bin-physical-resource-after-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /marker)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/fa6d3163fb794c8e7a17397ac8_raw.auxcpp.o',
                          'grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_program_bin-physical-python-before-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'PY_SRCS(conftest.py)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/objcopy_4cccb6d65bbce96629ca3408b2.o',
                          'grove/ledger/8d7321c2beeb41e9ca90318fd6_raw.auxcpp.o',
                          'grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_program_bin-physical-python-after-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(conftest.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/stone.cpp.o',
                          'grove/ledger/objcopy_4cccb6d65bbce96629ca3408b2.o',
                          'grove/ledger/8d7321c2beeb41e9ca90318fd6_raw.auxcpp.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_program_bin-physical-sandwich-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /marker)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(conftest.py)\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /tail)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/fa6d3163fb794c8e7a17397ac8_raw.auxcpp.o',
                          'grove/ledger/c0d2d593f9fb9c123547691141_raw.auxcpp.o',
                          'grove/ledger/stone.cpp.o',
                          'grove/ledger/objcopy_4cccb6d65bbce96629ca3408b2.o',
                          'grove/ledger/8d7321c2beeb41e9ca90318fd6_raw.auxcpp.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_program_bin-generated-explicit-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.cpp)\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(conftest.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/formed.cpp.o',
                          'grove/ledger/objcopy_4cccb6d65bbce96629ca3408b2.o',
                          'grove/ledger/8d7321c2beeb41e9ca90318fd6_raw.auxcpp.o']}]},
 {'name': 'py3_program_bin-generated-later-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(conftest.py)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.cpp)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/formed.cpp.o',
                          'grove/ledger/objcopy_4cccb6d65bbce96629ca3408b2.o',
                          'grove/ledger/8d7321c2beeb41e9ca90318fd6_raw.auxcpp.o']}]},
 {'name': 'library-pending-global',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RUN_PROGRAM(tools/writer IN ${BINDIR}/seed.dat OUT_NOAUTO receipt.dat '
                                    'formed.cpp)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO seed.dat)\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/formed.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']}]},
 {'name': 'library-resource-pending',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RESOURCE(formed.txt /payload)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.txt)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          '$(B)/grove/ledger/objcopy_78fbb545110f9a44eb1cf169f8.o']},
             {'output': '$(B)/grove/ledger/libledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'library-resource-ready',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.txt)\n'
                                    'RESOURCE(formed.txt /payload)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_78fbb545110f9a44eb1cf169f8.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'library-resource-chain',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RESOURCE(formed.txt /payload)\n'
                                    'RUN_PROGRAM(tools/writer IN ${BINDIR}/seed.dat OUT_NOAUTO receipt.dat '
                                    'formed.txt)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO seed.dat)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          '$(B)/grove/ledger/objcopy_78fbb545110f9a44eb1cf169f8.o']},
             {'output': '$(B)/grove/ledger/libledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'library-pending-global-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RUN_PROGRAM(tools/writer IN ${BINDIR}/seed.dat OUT_NOAUTO receipt.dat '
                                    'formed.cpp)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO seed.dat)\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/formed.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']}]},
 {'name': 'library-resource-pending-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RESOURCE(DONT_COMPRESS ${ARCADIA_BUILD_ROOT}/grove/ledger/formed.txt '
                                    '/payload)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.txt)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          '$(B)/grove/ledger/0796615e13aab9c6bab52ab55f_raw.auxcpp.o']},
             {'output': '$(B)/grove/ledger/libledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'library-resource-ready-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.txt)\n'
                                    'RESOURCE(DONT_COMPRESS ${ARCADIA_BUILD_ROOT}/grove/ledger/formed.txt '
                                    '/payload)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/0796615e13aab9c6bab52ab55f_raw.auxcpp.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'library-resource-chain-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RESOURCE(DONT_COMPRESS ${ARCADIA_BUILD_ROOT}/grove/ledger/formed.txt '
                                    '/payload)\n'
                                    'RUN_PROGRAM(tools/writer IN ${BINDIR}/seed.dat OUT_NOAUTO receipt.dat '
                                    'formed.txt)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO seed.dat)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          '$(B)/grove/ledger/0796615e13aab9c6bab52ab55f_raw.auxcpp.o']},
             {'output': '$(B)/grove/ledger/libledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-pending-global',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RUN_PROGRAM(tools/writer IN ${BINDIR}/seed.dat OUT_NOAUTO receipt.dat '
                                    'formed.cpp)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO seed.dat)\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/formed.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']}]},
 {'name': 'py3_library-resource-pending',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(formed.txt /payload)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.txt)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          '$(B)/grove/ledger/objcopy_78fbb545110f9a44eb1cf169f8.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-resource-ready',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.txt)\n'
                                    'RESOURCE(formed.txt /payload)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_78fbb545110f9a44eb1cf169f8.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-resource-chain',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(formed.txt /payload)\n'
                                    'RUN_PROGRAM(tools/writer IN ${BINDIR}/seed.dat OUT_NOAUTO receipt.dat '
                                    'formed.txt)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO seed.dat)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          '$(B)/grove/ledger/objcopy_78fbb545110f9a44eb1cf169f8.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-pending-global-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RUN_PROGRAM(tools/writer IN ${BINDIR}/seed.dat OUT_NOAUTO receipt.dat '
                                    'formed.cpp)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO seed.dat)\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/formed.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']}]},
 {'name': 'py3_library-resource-pending-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(DONT_COMPRESS ${ARCADIA_BUILD_ROOT}/grove/ledger/formed.txt '
                                    '/payload)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.txt)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          '$(B)/grove/ledger/0796615e13aab9c6bab52ab55f_raw.auxcpp.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-resource-ready-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.txt)\n'
                                    'RESOURCE(DONT_COMPRESS ${ARCADIA_BUILD_ROOT}/grove/ledger/formed.txt '
                                    '/payload)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/0796615e13aab9c6bab52ab55f_raw.auxcpp.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-resource-chain-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(DONT_COMPRESS ${ARCADIA_BUILD_ROOT}/grove/ledger/formed.txt '
                                    '/payload)\n'
                                    'RUN_PROGRAM(tools/writer IN ${BINDIR}/seed.dat OUT_NOAUTO receipt.dat '
                                    'formed.txt)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO seed.dat)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          '$(B)/grove/ledger/0796615e13aab9c6bab52ab55f_raw.auxcpp.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_program_bin-pending-global',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RUN_PROGRAM(tools/writer IN ${BINDIR}/seed.dat OUT_NOAUTO receipt.dat '
                                    'formed.cpp)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO seed.dat)\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/formed.cpp.o',
                          'grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          'grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']}]},
 {'name': 'py3_program_bin-resource-pending',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(formed.txt /payload)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.txt)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/stone.cpp.o',
                          'grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          'grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          'grove/ledger/objcopy_78fbb545110f9a44eb1cf169f8.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_program_bin-resource-ready',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.txt)\n'
                                    'RESOURCE(formed.txt /payload)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/objcopy_78fbb545110f9a44eb1cf169f8.o',
                          'grove/ledger/stone.cpp.o',
                          'grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          'grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_program_bin-resource-chain',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(formed.txt /payload)\n'
                                    'RUN_PROGRAM(tools/writer IN ${BINDIR}/seed.dat OUT_NOAUTO receipt.dat '
                                    'formed.txt)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO seed.dat)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/stone.cpp.o',
                          'grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          'grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          'grove/ledger/objcopy_78fbb545110f9a44eb1cf169f8.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_program_bin-pending-global-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RUN_PROGRAM(tools/writer IN ${BINDIR}/seed.dat OUT_NOAUTO receipt.dat '
                                    'formed.cpp)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO seed.dat)\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/formed.cpp.o',
                          'grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          'grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']}]},
 {'name': 'py3_program_bin-resource-pending-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(DONT_COMPRESS ${ARCADIA_BUILD_ROOT}/grove/ledger/formed.txt '
                                    '/payload)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.txt)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/stone.cpp.o',
                          'grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          'grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          'grove/ledger/0796615e13aab9c6bab52ab55f_raw.auxcpp.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_program_bin-resource-ready-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.txt)\n'
                                    'RESOURCE(DONT_COMPRESS ${ARCADIA_BUILD_ROOT}/grove/ledger/formed.txt '
                                    '/payload)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/0796615e13aab9c6bab52ab55f_raw.auxcpp.o',
                          'grove/ledger/stone.cpp.o',
                          'grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          'grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_program_bin-resource-chain-raw',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_PROGRAM_BIN(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(DONT_COMPRESS ${ARCADIA_BUILD_ROOT}/grove/ledger/formed.txt '
                                    '/payload)\n'
                                    'RUN_PROGRAM(tools/writer IN ${BINDIR}/seed.dat OUT_NOAUTO receipt.dat '
                                    'formed.txt)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO seed.dat)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/stone.cpp.o',
                          'grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          'grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          'grove/ledger/0796615e13aab9c6bab52ab55f_raw.auxcpp.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-physical-sandwich-pic',
  'mode': 'pic',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(- marker=1)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'RESOURCE(- tail=1)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_2b460f8205f174fff41e6103ee.o',
                          '$(B)/grove/ledger/objcopy_2b023d41aaf5c744a58ad08180.o',
                          '$(B)/grove/ledger/stone.cpp.pic.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.pic.o']}]},
 {'name': 'py3_library-physical-sandwich-root-first',
  'mode': 'root-first',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(- marker=1)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'RESOURCE(- tail=1)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_2b460f8205f174fff41e6103ee.o',
                          '$(B)/grove/ledger/objcopy_2b023d41aaf5c744a58ad08180.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-physical-sandwich-root-last',
  'mode': 'root-last',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(- marker=1)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'RESOURCE(- tail=1)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_2b460f8205f174fff41e6103ee.o',
                          '$(B)/grove/ledger/objcopy_2b023d41aaf5c744a58ad08180.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-physical-sandwich-raw-pic',
  'mode': 'pic',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /marker)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(conftest.py)\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /tail)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/fa6d3163fb794c8e7a17397ac8_raw.auxcpp.pic.o',
                          '$(B)/grove/ledger/c0d2d593f9fb9c123547691141_raw.auxcpp.pic.o',
                          '$(B)/grove/ledger/stone.cpp.pic.o',
                          '$(B)/grove/ledger/objcopy_4cccb6d65bbce96629ca3408b2.o',
                          '$(B)/grove/ledger/8d7321c2beeb41e9ca90318fd6_raw.auxcpp.pic.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.pic.o']}]},
 {'name': 'py3_library-physical-sandwich-raw-root-first',
  'mode': 'root-first',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /marker)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(conftest.py)\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /tail)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/fa6d3163fb794c8e7a17397ac8_raw.auxcpp.o',
                          '$(B)/grove/ledger/c0d2d593f9fb9c123547691141_raw.auxcpp.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_4cccb6d65bbce96629ca3408b2.o',
                          '$(B)/grove/ledger/8d7321c2beeb41e9ca90318fd6_raw.auxcpp.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-physical-sandwich-raw-root-last',
  'mode': 'root-last',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /marker)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(conftest.py)\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /tail)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/fa6d3163fb794c8e7a17397ac8_raw.auxcpp.o',
                          '$(B)/grove/ledger/c0d2d593f9fb9c123547691141_raw.auxcpp.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_4cccb6d65bbce96629ca3408b2.o',
                          '$(B)/grove/ledger/8d7321c2beeb41e9ca90318fd6_raw.auxcpp.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'library-generated-explicit-pic',
  'mode': 'pic',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.cpp)\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/formed.cpp.pic.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']}]},
 {'name': 'library-generated-explicit-root-first',
  'mode': 'root-first',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.cpp)\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/formed.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']}]},
 {'name': 'library-generated-explicit-root-last',
  'mode': 'root-last',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.cpp)\n'
                                    'SRCS(GLOBAL ${BINDIR}/formed.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/formed.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']}]},
 {'name': 'sandbox-helper-root-first',
  'mode': 'root-first',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'SANDBOX_PY3_TASK(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_c762f8f449cbb91b1788518fa8.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']},
             {'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/objcopy_639116a3578fed303deb18098b.o',
                          'grove/ledger/objcopy_07d68a3803a5a6941af7be05cd.o',
                          'grove/ledger/objcopy_b7f3092e05c278f37f7c0811c2.o',
                          'grove/ledger/stone.cpp.o',
                          'grove/ledger/objcopy_18389be8963e7a572e1e5fae39.o',
                          'grove/ledger/objcopy_02a2ebde23b74be2cc6849a98c.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'sandbox-helper-root-last',
  'mode': 'root-last',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'SANDBOX_PY3_TASK(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_c762f8f449cbb91b1788518fa8.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']},
             {'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/objcopy_639116a3578fed303deb18098b.o',
                          'grove/ledger/objcopy_07d68a3803a5a6941af7be05cd.o',
                          'grove/ledger/objcopy_b7f3092e05c278f37f7c0811c2.o',
                          'grove/ledger/stone.cpp.o',
                          'grove/ledger/objcopy_18389be8963e7a572e1e5fae39.o',
                          'grove/ledger/objcopy_02a2ebde23b74be2cc6849a98c.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-physical-sandwich-arm',
  'mode': 'arm',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(- marker=1)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'RESOURCE(- tail=1)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_2b460f8205f174fff41e6103ee.o',
                          '$(B)/grove/ledger/objcopy_2b023d41aaf5c744a58ad08180.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-physical-sandwich-raw-arm',
  'mode': 'arm',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /marker)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(conftest.py)\n'
                                    'RESOURCE(DONT_COMPRESS conftest.py /tail)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/fa6d3163fb794c8e7a17397ac8_raw.auxcpp.o',
                          '$(B)/grove/ledger/c0d2d593f9fb9c123547691141_raw.auxcpp.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_4cccb6d65bbce96629ca3408b2.o',
                          '$(B)/grove/ledger/8d7321c2beeb41e9ca90318fd6_raw.auxcpp.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'library-resource-source-shadow-False',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RESOURCE(formed.txt /payload)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.txt)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/formed.txt': 'physical shadow\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_78fbb545110f9a44eb1cf169f8.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'library-resource-source-shadow-True',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RESOURCE(${ARCADIA_ROOT}/grove/ledger/formed.txt /payload)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.txt)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/formed.txt': 'physical shadow\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_98846a79156978357fe1bcf4cf.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-resource-source-shadow-False',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(formed.txt /payload)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.txt)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/formed.txt': 'physical shadow\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_78fbb545110f9a44eb1cf169f8.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'py3_library-resource-source-shadow-True',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RESOURCE(${ARCADIA_ROOT}/grove/ledger/formed.txt /payload)\n'
                                    'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat formed.txt)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/formed.txt': 'physical shadow\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_98846a79156978357fe1bcf4cf.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'host-resource-runner-host',
  'mode': 'host',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RUN_PROGRAM(tools/runner OUT_NOAUTO receipt.dat formed.txt)\n'
                                    'RESOURCE(formed.txt /from_host)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'tools/runner/ya.make': 'PROGRAM(runner)\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RESOURCE(- host=1)\n'
                                    'SRCS(GLOBAL entry.cpp local.cpp)\n'
                                    'PY_SRCS(host.py)\n'
                                    'END()\n',
            'tools/runner/entry.cpp': '',
            'tools/runner/local.cpp': '',
            'tools/runner/host.py': 'host=1\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_61b264d897b95f2de5462df42d.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']},
             {'output': '$(B)/tools/runner/runner',
              'kind': 'LD',
              'members': ['tools/runner/objcopy_93c798c21f72c1cbd461c17a0e.o',
                          'tools/runner/entry.cpp.pic.o',
                          'tools/runner/objcopy_d8b5ce055f1b36c895954edd94.o',
                          'tools/runner/objcopy_fcfbed6f86247e19b2a028871d.o',
                          '$(B)/tools/runner/local.cpp.pic.o']}]},
 {'name': 'host-resource-runner-arm-host',
  'mode': 'arm-host',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'RUN_PROGRAM(tools/runner OUT_NOAUTO receipt.dat formed.txt)\n'
                                    'RESOURCE(formed.txt /from_host)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'tools/runner/ya.make': 'PROGRAM(runner)\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'RESOURCE(- host=1)\n'
                                    'SRCS(GLOBAL entry.cpp local.cpp)\n'
                                    'PY_SRCS(host.py)\n'
                                    'END()\n',
            'tools/runner/entry.cpp': '',
            'tools/runner/local.cpp': '',
            'tools/runner/host.py': 'host=1\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_61b264d897b95f2de5462df42d.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']},
             {'output': '$(B)/tools/runner/runner',
              'kind': 'LD',
              'members': ['tools/runner/objcopy_93c798c21f72c1cbd461c17a0e.o',
                          'tools/runner/entry.cpp.pic.o',
                          'tools/runner/objcopy_d8b5ce055f1b36c895954edd94.o',
                          'tools/runner/objcopy_fcfbed6f86247e19b2a028871d.o',
                          '$(B)/tools/runner/local.cpp.pic.o']}]},
 {'name': 'cpp-namespace-default',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']}]},
 {'name': 'cpp-namespace-custom',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'SET(MODULE_TAG CUSTOM)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_c3b31e4863e421cf8e34b12304.o',
                          '$(B)/grove/ledger/objcopy_471d73e8b4682c5d05e8c6e111.o']}]},
 {'name': 'cpp-namespace-empty',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SET(PYTHON3 yes)\n'
                                    'SET(_PYTHON_VER py3)\n'
                                    'SET(MODULE_TAG )\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_c78ca8d4759ddceca81a20ecd8.o',
                          '$(B)/grove/ledger/objcopy_b4749e750fd2c5f39483f11a16.o']}]},
 {'name': 'shared-owner-root-first',
  'mode': 'root-first',
  'files': {'other/ya.make': 'PY3_PROGRAM_BIN(other)\n'
                             'NO_PYTHON_INCLUDES()\n'
                             'SET(PEERDIR_TAGS SHARED)\n'
                             'PEERDIR(grove/ledger)\n'
                             'END()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'SANDBOX_PY3_TASK(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'IF(MODULE_TAG == "SB_TASK_BIN")\n'
                                    'PY_SRCS(note.py)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'ELSE()\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'ENDIF()\n'
                                    'SET(MODULE_TAG SHARED)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_885e93796ca3bbf575b7c36832.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_ba73f5021584eed310453e571d.o',
                          '$(B)/grove/ledger/objcopy_fbb2c9ef548d908d572b587385.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']},
             {'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/objcopy_9f36babc3d6025d48c1c6de427.o',
                          'grove/ledger/objcopy_635669e611393c5f86d894a453.o',
                          'grove/ledger/objcopy_885e93796ca3bbf575b7c36832.o',
                          'grove/ledger/objcopy_ba73f5021584eed310453e571d.o',
                          'grove/ledger/objcopy_fbb2c9ef548d908d572b587385.o',
                          'grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'shared-owner-root-last',
  'mode': 'root-last',
  'files': {'other/ya.make': 'PY3_PROGRAM_BIN(other)\n'
                             'NO_PYTHON_INCLUDES()\n'
                             'SET(PEERDIR_TAGS SHARED)\n'
                             'PEERDIR(grove/ledger)\n'
                             'END()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'SANDBOX_PY3_TASK(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'IF(MODULE_TAG == "SB_TASK_BIN")\n'
                                    'PY_SRCS(note.py)\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'ELSE()\n'
                                    'SRCS(GLOBAL stone.cpp local.cpp)\n'
                                    'PY_SRCS(note.py)\n'
                                    'ENDIF()\n'
                                    'SET(MODULE_TAG SHARED)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_885e93796ca3bbf575b7c36832.o',
                          '$(B)/grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/objcopy_ba73f5021584eed310453e571d.o',
                          '$(B)/grove/ledger/objcopy_fbb2c9ef548d908d572b587385.o']},
             {'output': '$(B)/grove/ledger/libpy3ledger.a',
              'kind': 'AR',
              'members': ['$(B)/grove/ledger/local.cpp.o']},
             {'output': '$(B)/grove/ledger/ledger',
              'kind': 'LD',
              'members': ['grove/ledger/objcopy_9f36babc3d6025d48c1c6de427.o',
                          'grove/ledger/objcopy_635669e611393c5f86d894a453.o',
                          'grove/ledger/objcopy_885e93796ca3bbf575b7c36832.o',
                          'grove/ledger/objcopy_ba73f5021584eed310453e571d.o',
                          'grove/ledger/objcopy_fbb2c9ef548d908d572b587385.o',
                          'grove/ledger/stone.cpp.o',
                          '$(B)/grove/ledger/local.cpp.o']}]},
 {'name': 'tasklet-declared-src-before',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'TASKLET_REG(Leaf py grove.Impl)\n'
                                    'PY_SRCS(note.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/Leaf.task.cpp.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o']}]},
 {'name': 'tasklet-declared-src-after',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'PY_SRCS(note.py)\n'
                                    'TASKLET_REG(Leaf py grove.Impl)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          '$(B)/grove/ledger/Leaf.task.cpp.o']}]},
 {'name': 'tasklet-declared-src-between',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'PY_SRCS(note.py)\n'
                                    'RESOURCE(- marker=1)\n'
                                    'TASKLET_REG(Leaf py grove.Impl)\n'
                                    'PY_SRCS(extra.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_2b460f8205f174fff41e6103ee.o',
                          '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
                          '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
                          '$(B)/grove/ledger/Leaf.task.cpp.o',
                          '$(B)/grove/ledger/objcopy_1f8c35ba3d8c9886686b2e6543.o',
                          '$(B)/grove/ledger/objcopy_03e3e4e7cdb92c11fb47902789.o']}]},
 {'name': 'tasklet-declared-src-resource',
  'mode': '',
  'files': {'other/ya.make': 'LIBRARY(other)\nSRCS(other.cpp)\nEND()\n',
            'other/other.cpp': '',
            'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'TASKLET_REG(Leaf py grove.Impl)\n'
                                    'RESOURCE(- marker=1)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n'},
  'groups': [{'output': '$(B)/grove/ledger/libpy3ledger.global.a',
              'kind': 'GA',
              'members': ['$(B)/grove/ledger/objcopy_2b460f8205f174fff41e6103ee.o',
                          '$(B)/grove/ledger/Leaf.task.cpp.o']}]}]
