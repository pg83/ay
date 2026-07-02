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

## Мультимодуль: остаток после введения ModuleUnit (2026-07-02)

Сделано: resolveModuleUnit(stmt, kind, language) — юнит (Type, MODULE_TAG,
CC-тег, AR-префикс, global-AR-тег, sbom-lang) резолвится один раз в collect;
инстансы вариантов уже существовали (PY3_PROGRAM: bin PEERDIR'ит собственную
директорию → paired-lib инстанс; PROTO_LIBRARY: ось Language). Убиты
programPairedLib, resource*TagForData, cfModuleTag/moduleCCTag, литералы
"PY3"/"PY3_PROTO", per-site switch'и AR-префиксов/тегов, мёртвые
arInstance/ldInstance.Language-переписывания.

Остаток:
- applyDeferredPeerOrder (gen_hacks.go) — довести до d.latePeerdirs на
  collect-этапе: отложенные `when PEERDIR+=` upstream-конфа = поздние peerdirs
  юнита, а py3Program-ветка должна выпасть из честной пары юнитов;
- PY23_LIBRARY/PY23_NATIVE_LIBRARY: py2-вариант не строится вовсе;
- hOnly-артефакт py-proto: objcopy-глобальный AR того блока живёт с default
  "lib"/tagGlobal — при развязке юнитов должен стать артефактом своего юнита;
- pyNamespaceUnitType — множество типов юнита, кандидат в поле ModuleUnit.
