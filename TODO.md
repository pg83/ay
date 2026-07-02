# TODO

## Спец-перестановки genModule — итог разбора (2026-07-02)

Из восьми ad-hoc перестановок после эксперимента «убрать всё → гейт»:

- удалены как мёртвые на срезах (гейт не заметил): runtime_py3 addIncl-splice,
  фильтр malloc/api при ALLOCATOR(FAKE) (upstream-форма «не добавлять» уже есть
  в suppressMallocAPIDefault), jemalloc-переносы вокруг cowOn
  (+ хелперы moveArchivePathsAfter/movePathsAfter/moveSubrangeToFront);
- py2/py3-хвосты archiveOrder и cflagsAggOrder-свитч заменены
  applyDeferredPeerOrder (gen_hacks.go) на СБОРКЕ allPeers; py2/bin-ветка
  обоснована upstream-механизмом (отложенные `when PEERDIR+=` коммитятся на END
  модуля), py3Program-ветка — эмпирика сплющенного мультимодуля (runtime_py3/main
  в upstream — немедленный PEERDIR() в теле conf; перенос program-defaults за
  пользовательские peers из отложенности не выводится). Честная форма —
  d.latePeerdirs на collect-этапе + развязка мультимодуля (см. раздел ниже);
  archiveOrder/sbomOrder/cflagsAggOrder переменные умерли, всё итерирует resolved;
- objcopy-lead в глобальном AR заменён прямой категорийной сборкой
  [RESOURCE-objcopy, GLOBAL srcs, pySrc-trail] из уже раздельных частей
  (e.objcopyRes/e.globalRefs); порядок категорийный, не декларационный
  (проверено фикстурой GLOBAL_SRCS-до-RESOURCE); условие len(d.resources)>0
  для позиции kv-only нод — эмпирика, требует upstream-подтверждения;
- sbom-порядок (cxxsupp после libcxx; lld после аллокаторных peers; own-компонент
  py3-программы перед lld) сведён в applySbomComponentOrder + ownSbomInsertIdx —
  ЭМПИРИКА эталонных графов: source link_sbom/сборки sbom-списка в чекауте
  upstream отсутствует (internal). Гипотеза для проверки при доступе к source:
  порядок sbom-компонент — post-order обхода peer-замыкания.

## Слой R: незамоделированные ветки upstream resource_handler

- yasm-паковщик (TYASMResourcePacker → ro_<hash>.rodata) не смоделирован: все наши
  потоки эквивалентны DONT_COMPRESS/не-x86-веткам, где upstream берёт raw. Нужен,
  если в срезах появится RESOURCE без DONT_COMPRESS на x86-64 вне py-стека.
- raw-фолбэк для не-CanHandle записей обычного RESOURCE (пути с ARCADIA_BUILD_ROOT):
  сейчас packResources кидает throw при отсутствии RawClosure; upstream пакует
  через raw. Реализовать при первом реальном кейсе.

## Мультимодуль: несколько upstream-юнитов сплющены в один ModuleData

В upstream PROTO_LIBRARY / PY3_PROGRAM — мультимодули: каждый вариант это
отдельный юнит со своим MODULE_TAG и своим конфигом (CPP_PROTO, PY3_PROTO,
PY3_BIN, PY3_BIN_LIB, ...). У нас частично это выражено осью
ModuleInstance.Language (LangCPP/LangPy/LangDescProto для proto — отдельные
инстансы с memo), а частично — сплющено в один ModuleData и разруливается
ad-hoc данными по месту:

- resourceBinTagForData vs resourceLibTagForData: bin- и lib-стороны
  PY3_PROGRAM — это два upstream-юнита (PY3_BIN / PY3_BIN_LIB), у нас — выбор
  тега per-call внутри одного модуля;
- d.programPairedLib, py3BinProgramSide: флаги-костыли той же пары;
- "PY3_PROTO"-тег py-proto пака и CPP_PROTO-override в emitResourceObjcopy:
  MODULE_TAG вариантов, передаваемый параметром вместо юнитного свойства;
- hOnly-ветвление результата specialized-модулей: результат одного варианта,
  собранный внутри чужого прохода.

Направление: довести ось вариантов до конца — вариант = собственный
ModuleInstance (как уже сделано для Language) со своим MODULE_TAG/env/результатом;
тогда per-call теги, парные флаги и hOnly-сборка схлопываются, а packResources
получает Tag из юнита, как в upstream.
