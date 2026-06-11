# ay codebase study notes (exam prep, 2026-06-11)

ay = реимплементация ymake (графопостроитель ya) на Go, package main, ~54k LOC (34k без тестов).
Цель (GOALS.md): sg5 байт-в-байт; upstream — /home/pg/monorepo/yatool (devtools/ymake, devtools/ya/bin).

## Пайплайн (make -j0 -G)

main.go: dispatch → parseGlobalFlags (--probe=map|callsite|str) → runCommand{fetch,make,dump,perf,refac,probe}.
make.go cmdMake: `make cas` → cas_analyze; GC: -j0 → SetGCPercent(-1) (GC off, −20% user time, RSS 495→680MB), иначе 400.
ya.conf TOML (host_platform_flags/flags) + build/internal/ya.conf (если testLevel==0) + toolchainFlags (mine.go: CLANG_VER=20, OS_SDK=ubuntu-16, USE_ARCADIA_PYTHON...) → host/target флаги; host: PIC=yes, GG_BUILD_TYPE=release default; target: PIC=no, buildType из -r/-d/--xbuild, TESTS_REQUESTED (-t), SANDBOXING.
→ newPlatform ×2 → genStream/genDumpGraphWithResources → runGenIntoWithResources:
  buildScriptTable (script_deps.go: build/scripts/*.py import closure, scripts[v][0]==v)
  → ResourceAwareEmitter(плюс fetchRefs map) обёртка над Buffered/StreamingEmitter
  → newIncludeParserManagerFS + 2 CodegenRegistry + 2 IncludeScanner (host/target; общий ctx.tarjan)
  → seed = ModuleInstance{source(target), KindBin, LangCPP, targetP} → genModule → ctx.emit.result(root.LDRef)
  → testMode: emitTestRunNodes; mergeGeneratedFirstClaims → BufferedEmitter (target wins).
-j>0: executor (newExecutor, ex.onNode streaming), ex.run, failedRoots, installRoot.

## Platform (platform.go)

Поля precomputed: Flags map[ENV]STR (internFlags — единственная строковая граница), CCHead=[--target=triple, -march, sysrootArgs], WrapccHead/Tail (OPENSOURCE=yes → nil), CCUsesResources=[CLANG<ver>, +YMAKE_PYTHON3 при wrapcc], DebugInfoFlags/CompileCFlags, CompressDebugSections (gzZstdRule = "debug_info_flags.append('-gz=zstd')" в build/ymake_conf.py репозитория ydb, у yatool нет), MultiarchLibPathSTR, SystemLibs (MUSL: -nostdlib -lm; else -nodefaultlibs -lpthread -lc -lm; prelude -ldl -lrt), Triple=isa-os-gnu, marchFor(aarch64)=armv8-a. sysrootArgsFor: OS_SDK=local/OPENSOURCE → [-B/usr/bin]; иначе --sysroot=$(B)/resources/OS_SDK_ROOT (+MUSL → /nowhere). Ragel6Optimized = release && !sanitized.

## genModule (gen.go:598) — сердце

memo: IntValueMap по instanceKey = Path<<16|Kind<<8|Language<<1|hostbit (fail-fast на чужой Platform). walking map → PEERDIR-циклы толерируются (пустой результат, НЕ мемоизируется). parseFile(ya.make) → buildIfEnv (DefaultIfEnv.clone + Platform.Flags + ARCH_*, CURDIR/MODDIR/BINDIR, SSE-флаги при x86_64 && !DISABLE_INSTRUCTION_SETS) → collectModule.
- LangPy не-PROTO_LIBRARY → реентер как LangCPP, py-ключ алиасится (единая точка reenter-with-corrected-parameters; streaming emitter не умеет retract).
- PROTO_LIBRARY: CPP-вариант — re-collect с MODULE_TAG=CPP_PROTO+GEN_PROTO; Py — PY3_PROTO.
- RESOURCES_LIBRARY → bindResourceGlobalVars (+re-collect если bound) → genResourcesLibrary.
- Авто-peer'ы: .ev → eventlog+protobuf; PROTO_LIBRARY+.proto → contrib/libs/protobuf (+optimizePyProtos default on); py proto → contrib/python/protobuf (+grpcio при GRPC); .fbs → contrib/libs/flatbuffers (после явных); PY3_PROGRAM(_BIN) → runtime_py3/main, import_tracing/constructor, testing/import_test (early/late, _BIN сплайсит после contrib/libs/python); pyLibraryAutoPythonPeer → contrib/libs/python prepend; GENERATE_ENUM_SERIALIZATION → enum_serialization_runtime в ТЕКСТОВОЙ позиции макроса (modules.go).
- Specialized (PROTO_LIBRARY/DLL/SO_PROGRAM/DYNAMIC_LIBRARY): walkPeersForGlobalAddIncl (отдельный обход) → header-only путь: emitRunProgramsForAR/PythonForAR, emitPySrcs, emitResourceObjcopy (+AR global lib<py3|py3c>...global.a по типу), emitMiscNodes, emitProtoSrcs, ранний return.
- Общий путь: allPeers = languageDefaults + unitTestPeer + preUserProgDefaults + allocatorPeers[name] + postUserProgDefaults + d.peerdirs, peerSeen через deduper(internStr(p)<<1); peerKind {lang,program,user,unittest} — порядок агрегации addincl load-bearing.
- resolved → ResourceGlobalClosure (dedup по GlobalVar) → d.tc = resolveModuleToolchain.
- archiveOrder: PY2_PROGRAM/PY3_PROGRAM_BIN — contrib/libs/python+runtime_py3 в хвост; PY3_PROGRAM — programTail (program-default+allocator) и pythonTail.
- Пять deduper-проходов: peerArchive (closure paths затем own AR), peerGlobal, peerWholeArchive, peerWholeArchiveCmd, peerDynamic; peerLinkCmd = union в interleaved порядке.
- peerAddInclGlobal: lang(Own затем транзит) → unittest → program → user (UserGlobal → ONE_LEVEL track → транзит GLOBAL); oneLevelOnlyPaths не репропагируются (один хоп). libraryPythonRuntimePy3 хак: bld dir после abseil.
- ProtoAddInclGlobal (_PROTO__INCLUDE, только PROTO_NAMESPACE GLOBAL или PROTO_LIBRARY) vs ProtoNamespaceTail (не-GLOBAL, хвост, только moduleTag==0 потребители).
- dedupedAddIncl = own + global (TModuleIncDirs: GLOBAL не исключает own resolve).
- moduleInputs (ModuleCCInputs) + CCBlocks = composeCCModuleArgBlocks ОДИН раз на модуль.
- Pass 1: codegen-producing srcs (.proto/.fbs/.ev/.rl/.rl6/.y/.cpp.in/.c.in — isCodegenProducingSrc) эмитятся ДО emitCopyFiles/emitEnumSrcs/emitMiscNodes (shared childrenCache — иначе stale «no pb.h» закешируется). Pass 2: не-codegen srcs, потом codegenEmits, потом checkConfigH, cython, swig, jv, en, pr, py, simd (FlatOutput+Variant), joinSrcs (JOIN_SRCS: jsCCIncludeInputs; x86_64 — rebase peer addincl), globalSrcs + pyRegister + genPyAux + objcopy (RESOURCE перед SRCS GLOBAL при d.resources).
- PROGRAM: FAKE allocator → выкинуть library/cpp/malloc/api; PY3_PROGRAM+J → moveArchivePathsAfter/Before хаки (jemalloc после cow/on, enum_ser_runtime перед json/common); reorderLDMembers (/_/_/ legacy в хвост); emitLD; ldPath; UNITTEST_FOR+testMode → buildTestSuiteInfo.
- LIBRARY: reorderARMembers порядок: noLto, regular, cf, join, g4, h_serialized, evPb, pbCC(только из .proto SRCS), rl6, reg3, legacyR6; emitARNamed(Tagged); global archive (tagGlobal/py3_global/py3_native_global/yql_udf_static_global/py3_bin_lib_global при programPairedLib).
- ctx.tool/toolResult: tools DenseMap[ARG]; кеш только при LDRef!=0 (cycle stub не кешируется!); moduleByRef.put для INDUCED_DEPS сканера.

## collectModule / collectStmts (modules.go)

env.setString(MODDIR/CURDIR/BINDIR) → d{pythonSQLite3:true, bisonGenExt:cpp} → collectStmts (switch по Stmt типам; UnknownStmt → applyUnknownStmt с ~70 typed-ветками: ADDINCLSELF, NO_*, USE_LLVM_BC*, LLVM_BC, CHECK_CONFIG_H, BUILDWITH_CYTHON_*, GRPC, PY_NAMESPACE, YQL_ABI, PROTOC_FATAL_WARNINGS, USE_COMMON_GOOGLE_APIS (prepend googleapis), FLATC_FLAGS, COPY_FILE (AUTO → в srcs), PROTO_NAMESPACE (build addincl + GLOBAL), EXCLUDE_TAGS, ALLOCATOR, ARCHIVE, ENABLE/DISABLE (env.setBool + спец-флаги), SRC/SRC_C_NO_LTO (flatSrcs), SRC_C_AVX2... (simdSrcs), LD/AR_PLUGIN, EXPORTS_SCRIPT, EXTRALIBS, PY_SRCS (TOP_LEVEL/NAMESPACE/CYTHON_*/SWIG_*/MAIN/.pyx/.pyi/__main__.py автодетект PY_MAIN), ALL_PY_SRCS, PY_MAIN, PY_CONSTRUCTOR, NO_CHECK_IMPORTS, CPP_PROTO_PLUGIN0/1/2, PY_REGISTER, SET_APPEND (SFLAGS/_PROTOC_FLAGS/RPATH_GLOBAL), INDUCED_DEPS (h/cpp/h+cpp бакеты); неизвестный макрос вне acknowledgedMacros → throw).
- SRCS: GLOBAL префикс-токен; yql-udf — все в global; ${...} непресолвленные skip; .h.in/.y → addGeneratedHeaderInclude (build dir в addIncl+Global+UserGlobal).
- CONFIGURE_FILE: .h.in/.h → addGeneratedHeaderInclude, иначе cfAddIncl (отложенный, мерджится в collectModule после collectStmts — upstream resolve order).
- После collectStmts: srcDirs seed [dirKey(modulePath)]; cf addincl merge; filterInvalidAddIncl (filterExistingSourceDirs + addInclUserGlobal пересборка); MUSL/NO_STRIP env; applyPython3AddIncl (usePython3 → -DUSE_PYTHON3, pythonIncludeDir); moduleScopeCFlags = musl + sseBase prefix; dedupVFS addincl; pm.indexAddincl.
- expandStmtToken: $S/$B спец; ≤8 итераций: bare $NAME (только весь токен), expandEmbeddedDollarVars ($NAME в середине), expandBracedVars (${NAME}); unresolved → literal остаётся. expandStmtTokensSTR: fast path — нет '$' → тот же STR id; иначе expand + Fields split + intern.
- buildIfEnv: OPENSOURCE→YA_OPENSOURCE/CATBOOST_OPENSOURCE; питание SSE*_CFLAGS.
- derivePeerInstance: KindLib, peerEntryLanguage (py для python-консьюмеров; reenter в genModule страхует).

## Scanner (scanner.go) — include engine

IncludeScanner (на платформу): sysincl *SysinclCtx; scanCache DenseMap3[STR→children []VFS, ClosureRef, sourceFileExists bool] — ключ ТОЛЬКО includer strID, без scan-context (инвариант upstream OnceProcessedAsFile, add_iter.h:377; добавить контекст в ключ = коллапс кеша, на порядок медленнее); subgraphClosures [][]VFS (sub-slices bump-арены closureArena, hint 8192 = 2×max sg5 closure 3935; index 0 reserved); searchTierFlat IntValueMap[splitMix64(ctxNum,target)] + searchTierSeen BitSet гейт; configByHash → ScanConfigEntry{ctxNum dense, ri *CfgResolveIndex}; sourceUnderCache IntValueMap[splitMix64(incDir,target)]→VFS (0=нет; хранение rel-строки реинтернило на каждом хите); dfsActive BitSet set-once; generatedFirstClaim map[VFS]string (Node2Module правило, json_visitor.cpp:638 — finalize в attribute_generated.go перепривязывает module_dir).

dfs (2 прохода из-за single-pending арены): pass1 closureOf каждого ребёнка (может рекурсировать/аллоцировать); цикл → dfsActive.has → tjc.runSCC (Tarjan strongconnect, SCC члены получают ОДИН общий ref). pass2: tjc.closure.reset, block=arena.alloc, self первым, spliceNew окон детей (windowSubsumed skip: окно уже целиком в блоке, leafEver исключение), closureLeaves сплайс для $(B) членов (COPY_FILE(TEXT) source+tooling, .proto источник pb.h — bare члены, не traversed), commit, putClosure.

resolve(includer, incDir, d): rooted target ($(S)/$(B)) → прямой bind (vfs(), без поиска). Иначе resolveSearchPath + sysincl.lookup; quoted + searchOut: bypass если !hasMultiTarget или searchOut[0] = same-dir; mappings мерджатся с existence-фильтром (sourceFileExists memo col3). Нерезолв → WarnMissingInclude (fail при !keep-going).

resolveSearchPath: out сам себе dedup (≤3 эл.); cythonPy2SiblingOverride хардкоды; includer $(B) && target с «/» → codegen.lookupRel; quoted → sourceUnderCache → lookupSplit(incDir, d.target) — incDir и есть ключ; иначе resolveContextSearchTier(d.target).

resolveContextSearchTier: seen-bit гейт → flat hit; miss: normalisePath; indexable (нет ADDINCL $(S) корня) → addinclIndex inverted (target→prefixes) + rank IntValueMap первый-выигрывает, build entries через lookupSplit по рангу; не indexable → линейно Own→Peer→Base addincl. BaseSearchPaths = [$(S), $(B), linux-headers, linux-headers-nf] (sources.go).

CodegenRegistry: byStr DenseMap[STR] (full-path id И bare-rel id оба ключа; lookupSTR, строковых клиентов нет), bySplit IntMap[splitKey=splitMix64(source-dir VFS, suffix STR)] на КАЖДЫЙ слэш rel (префикс = канонический $S-dir VFS, идентичность dirKey/OsFS.dirs; lookup-only internedPrefixed/internedBytes для $B-addincl конверсии), splitPrefixSeen BitSet гейт, leafEver BitSet. register throw на дубль. DeferredCF — ленивые CONFIGURE_FILE (emitCF при первом потребителе через resolveCodegenDepRefs probe).

sysincl (sysincl.go + sysincl_ctx.go): YAML-правила build/sysincl/*.yml по ISA/MUSL/OPENSOURCE; SysinclPair{key STR, keyCI string, paths []VFS} слайсы; buildSysinclIndex: intra-record last-wins (96 реальных дублей), byLower map → []SysinclContribution{paths, filter *SourceFilter, rawKey, order, ci, multi}. mightClaim: keyBits BitSet (CS) → ciMemo TwoBitSet first-touch memo (ciUnseen/ciNo/ciClaimed; CIMemoState) → ciClaims (len gate ciMaxLen, ciGate bitset по uint16(raw[0])*len+raw[1] в обоих регистрах, keyCI map). lookup: mightClaim гейт → merged.lookup(path, target.string()): ToLower bucket, CS — rawKey точное сравнение, filter.match(path).

## Emitters

Node: Cmds []Cmd{CmdArgs ArgChunks, Cwd, Env, Stdout STR}, Inputs InputChunks ([][]VFS zero-copy), Outputs, KV{P ProcKind, PC цвет,...}, DepRefs/ForeignDepRefs (tool deps, в finalize DFS не ходят), Tags, TargetProperties{ModuleDir,ModuleTag,ModuleLang,ModuleType}, Requirements, usesResources []string. ProcKind: CC AR LD PB PY EV EN CP CF AS R6 JV YC CY SW FL BC BI RI RD UN PR OP FT TS...
Emitter: Buffered (граф потом finalizeOrder Kahn topo + resolveAndUID) vs Streaming (резолв и эмит по мере готовности; uidScratch). UID: xxh3-128 канонической кодировки (CanonBuf: STR → 8-байт lo-half intern-таблицы; $(S) inputs + contentHash; DepRefs → их UID = Merkle); resourceFetchUID — стабильный от URI+output.
emitCC: cmd = [wrapccHead? src wrapccTail] + [CC|CXX] + CCHead + [-c -o out] + common + c/cxxTail + perSource + cPost + in. CcModuleArgBlocks once/module.
emitLD: 4-5 cmd (vcs_info.py → vcs compile → link_exe.py → link-or-copy; splitDwarf). Trailer: rdynamic, version-script, compress-debug, prelude, no-as-needed, rpath, fPIC, gdb-index, ldflags, system libs, strip-all, gc-sections, no-pie.
emitAR: ARCmdHead (link_lib.py llvm-ar gnu...) из toolchain.
proto: PbArgBlocks (head/mid/tail) once/module-proto-ctx; cpp_proto_wrapper.py + protoc + cpp_styleguide; grpc plugin; pb.h registered c directImports+descriptor+transitive.
Const-векторы per file: flatcConstFlags/flatcIOLeadArgs, evProtocConstArgs, yasmConstHead, cythonConstHead, antlrJavaConstHead, ragel6ConstArgs, swigConstArgs, rodataConst*.
ResourceAwareEmitter: usesResources → FETCH deps (fetchRefs), attachResourceDeps.
emit_py_objcopy: ObjcopyArgBlocks (objcopy.py --compiler CXX...); objcopyHash = MD5(sorted paths/keys/kvs/unitPath/tag)[:26]; RESOURCE pairs хранятся RAW (не expanded!) — хеш против ${BINDIR} формы.

## Executor (executor.go)

sema chan (j slots); byUID dedup; visit рекурсивно по deps; workspace tmp/<uid> + flock; sandboxing: X/s symlink declared $(S), X/b restore deps; CAS sha256 hardlink cas/<hh>/<hash>; uid/<u>/<uid> JSON манифест, atomic rename; grb/ trash + фоновый GC (1/сек); --cmd-prefix; --ya-*-command-file → @args файл; ninja vs ANSI repaint; tools hardlink (не symlink) под $(B)/resources.

## Dump / validate

normalize: 2 прохода streamGraphFanout; self_uid = sha256(canonContent)[:22] (intrinsic), uid = Merkle re-uid post-order; roots: LD output $(B)/<target>/, AR (предпочесть не-host), TS kv.path; FETCH/VCS strip; refGraph=true → filterARLDInputs + build-order-only dep strip (оба чека: depOutputInInputs И depOutputInCmd провалены → strip); normPath: $(BUILD_ROOT)→$(B), versioned $(NAME-id)→$(NAME), $(B)/resources/NAME→$(NAME).
sort: external merge (chunk 256MB, MergeHeap k-way).
diff: пары по output (exact: +kind+platform+host → axis → any); режимы summary/by-field(10 полей FNV)/by-token(категории incl/def/warn/march/fflag/path/UNEXPANDED)/by-kind/roots(leaf-most)/pair.
validate.py: кейсы sg2(devtools/ymake/bin@yatool aarch64 musl), sg2_x86_64, sg3(devtools/ya/bin), sg4(util/ut@ydb), sg5(ydb/apps/ydbd, perf gate 8.80s×1.2, 3 retry min), sg6(полная аркадия /home/pg/3, auto-xfail, отсутствует на хосте). Глобальный flock. Параметры: -j 0 -G --sandboxing.
dump_graph finalizeDumpGraph: strip standalone LLVM PR nodes (llvm16/include codegen scaffolding без потребителей).

## Intern / контейнеры

internTable: ids IntMap[STR] (hi64 xxh3-128, identity-hash), strs []string, los []uint64 (lo64 — верификация коллизий + uid hash), overflow map (hi-коллизии). STR.string() = view + probe hook. interned() — lookup-only. ARG/ENV/TOK: двухуровневые (DenseMap[STR]→dense id, strs []STR; .str() free). VFS = fullPathSTR<<1|rootBit; rel() = strs[strID][5:]; vfsBound = len(strs)<<1. counters: YATOOL_PERF_STATS=1; intern strs ~229.5k на sg5.
IntMap: open addressing, pow2, load 5/8, key 0 reserved. IntValueMap: значения в side slice. DenseMap (gen_densemap.py), DenseMap3 (один idx, 3 колонки, slot 0 sentinel, отдельная presence на колонку). IdSet: epoch-stamped, O(1) reset, spliceNew с hoisted locals. DeDuper deduper глобальный (leaf contract: reset→scan, без реентера; genModule сбрасывает между проходами!). BitSet/TwoBitSet zero-value-empty. BumpAllocator: chunks 1.5x, address-stable, alloc/commit single-pending.
TarjanCtx: scratch (stamp/index/low/onStack, epoch), stack, closure IdSet; ClosureSink интерфейс (forEachChild/cachedWindow/emitClosure/windowSubsumed); SCC члены делят один closure ref.
throw.go: throw/throw2/throw3/throwFmt/try/catch/newException/exceptionf; *Exception{what closure}; try re-panics не-Exception.

## Пробы / refac / lint

--probe=map (mapinstr: AST wrap mapKR/mapKW, throwaway, git checkout revert; КОММИТЬ перед запуском); --probe=callsite (recordCall в каждой func, CALLSITE_OUT); --probe=str (runtime.Caller(2) PC tally в STR.string(), резолв на дампе).
refac consts (5 фаз, хойст intern* литералов → vfs/str/arg/env_consts.go, dedup по resolved path); refac lint (consolidate-vars, expand-func-bodies, func-blank-lines, blank-around-blocks вкл return/defer/continue/break, tight-braces, case-convention отчёт); refac case (декларации AST + референсы по ошибкам компилятора -gcflags=-e, байтовые колонки, fixpoint ≤500, forbiddenLowerNames = predeclared+импорты, stdlib врапперы String/Error/MarshalJSON/.../Push/Pop/Unwrap — однострочные).
lint.sh: build → refac consts → refac lint → gofmt -w → build.
STYLE.md: types Upper / methods+functions lower; throw вместо if err != nil pass-through; blank lines вокруг control+flow statements; все .go в корне; JSON only config.

## Перф-история (эта сессия и раньше)

map ops 1,031,227 → 626,229 (−39.3%); .string() 1,367,433 → 648,953 (−52.5%): tier lazy string, codegenUnder split-only (canRelFilter fallback удалён юзером, тестовый харнесс получил пустой реестр), Stdout lo-hash в canon, sysincl ciMemo TwoBitSet (279k→35.9k, uniq 29,596, повтор 9.4x; keyCI map probes 31,890→4,761). Остаточный топ: sysincl lookup 74k (вариант 2: rawKeyID сравнение по id + bucket по id двухъярусно — прекомпьют CS + first-touch CI в ciClaims), tier miss 64k, expand $-fast-path 56k. Memory rules: gate foreground; не полагаться на host/target идентичность; closure arena hot — не сжимать (12.77x read amplification); RESOURCE pairs RAW.

## sysincl загрузка (sysincl.go)

sysInclYamlSequence — фиксированный порядок build/sysincl/*.yml: macro, libc-to-compat, libc-to-nothing, stl-to-libstdcxx, stl-to-nothing, windows, darwin, android, freebsd, freertos, intrinsic, nvidia, misc, unsorted, swig, libiconv, libidn, jdk-to-arcadia, [opensource.yml XOR proto.yml по OPENSOURCE], libc-to-musl/linux-musl-{aarch64,x86_64} (MUSL), emscripten-to-nothing, nvidia-cccl, stl-to-libcxx, libc-musl-libcxx (MUSL), python-2-disable*. supportedSysInclArchs = {aarch64, x86_64} (иначе throw). Внутренний контур (!opensource): build/internal/sysincl/*.yml ВСЕ, sorted, ПОСЛЕ базы (override, напр. taxi.yml errno.h→userver).
Record: source_filter (regexp; `(?!` → KeyBySource; неизвестный ключ → unsupported filter = record disabled), case_sensitive:false → CI (lower), includes: scalar → resolve-to-nothing (nil paths), map → header→scalar|seq|null. len(paths)>=2 → HasMultiTarget.
SourceFilter компиляция: альтернативы; literalPrefix / containsLit (`.*lit.*`) / regex с reGuard префиксом; literalAltsFromRegex разворачивает ^-anchored конкатенации/альтернации в ≤64 литералов (избегает regexp на хоте); negative lookahead → excludePrefixes.

## defaults.go детали

runtimeAncestorPaths (isRuntimeAncestor — у них PROGRAM-тип не «программа» для isProgram гейта): musl, libc_compat, linuxvdso(+original), cxxsupp/{builtins,libcxx,libcxxrt,libcxxabi,libcxxabi-parts}, libunwind, malloc/api, sanitizer/include, util. runtimeAncestorCxxConsumers={library/cpp/malloc/api} (получает -nostdinc++ hoist в genModule).
defaultPeerdirsFor: linux-headers ВСЕГДА (НЕ гейтится noPlatform — NEED_PLATFORM_PEERDIRS, _BASE_UNIT; не-C++ модуль получает ТОЛЬКО его); C++: cxxsupp/libcxx+libcxxrt+libunwind (!NoRuntime && !noPlatform), util (!NoUtil), musl/include (muslOn), sanitizer/include (useArcadiaCompilerRuntime: flags платформы, default true), build/platform/{clang,clang/clang-format,lld,python/ymake_python3} (всегда кроме build/platform/* самих; RESOURCES_LIBRARY, инертны, дают resource globals + LDFLAGS lld).
program defaults: pre-user = build/cow/on + tcmalloc+no_percpu_cache (!hadAllocator && linux && (musl||x86_64)); post-user = musl/full|musl (muslLite), cpuid_check (x86_64 && !NoUtil && allocator!=FAKE). suppressMallocAPIDefault при FAKE. rebasePerArchPeerAddIncl: contrib/libs/musl/arch/<isa> при JOIN_SRCS x86_64.
effectiveNoPlatform = NoPlatform || (NoLibc && NoRuntime && NoUtil). peerYaMakeExists гейт не-user peers.

## Гочи (быстрый список)

- deduper глобальный: НЕ переживает вложенный genModule — сбрасывать после рекурсий, leaf passes only.
- scanCache без контекста — менять ключ нельзя; чинить выше (parsedIncludes/sysincl/searchTier).
- tool cycle stub (LDRef==0) не кешировать.
- pass1/pass2 порядок srcs: codegen-producing раньше прочих эмиттеров (stale children cache).
- VFS root bit: $S/foo и $B/foo — разные intern-записи; dirKey всегда Source-rooted.
- Emitter streaming: нельзя retract — реентеры до эмиссии.
- evalCond: > и >= переписываются в < и !< на парсе; chained сравнения throw.
- expandStmtToken: одна подстановка, unresolved literal остаётся; потребители скипают «${».
- ENABLE folds в env bool; PY_SRCS МАИН/.pyx/.swg/.pyi разбор. PROTO_NAMESPACE GLOBAL vs bare → разные каналы пропагации.
- uid: writeSTR пишет lo-half (НЕ байты); $(B) контент через dep uid; $(S) через contentHash (faults если файл не читан).
- normalize refGraph асимметричен: наш граф faithful, реф прюнится.
- sg5 perf gate: 8.80s budget × 1.2; >— гонять 3 раза, брать min.
