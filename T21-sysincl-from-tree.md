# T21: SYSINCL and generated input closures

The source-driven SYSINCL stage is preserved in the coordinator's selected integration. This continuation fixes configuration exclusion, reuses scanner SCC results for COPY inputs, restores the YDB regression, and implements native SWIG/NLG input rules. ROOT owns combined full-gate acceptance and integration; this worktree does not push or modify master.

Upstream mechanisms:

- `~/1/devtools/ymake/conf.cpp:416` expands SYSINCL before loading YAML. `lang/confreader.cpp:588,925` loads imports and separates unconditional assignments from deferred actions; `lang/config_conditions.cpp:21,76` applies conditions against current values. The source list includes `~/1/build/conf/sysincl.conf` and all imported global assignments. Static AST/YAML records reuse the shared parse cache; ymake_conf.py still runs for every required platform.
- `~/1/devtools/ymake/vars.h:227` repeatedly removes substrings for unconditional `-=`. Commit025f0b7 replaces word exclusion and tests both notfirst→first and a deletion that exposes another match.
- `~/1/devtools/ymake/json_visitor.cpp:575` shares an input set for nodes in one graph loop. Commit88f1487 records COPY source facts during the existing Tarjan SCC scan in GeneratedFileInfo, removing repeated reachability traversals. Commitb0830be preserves facts when lazy emission causes a component to be rediscovered with cached members.
- `~/1/devtools/ymake/include_processors/swig_processor.cpp` resolves native includes with the SWIG language id. Commit9b27251 selects existing SWIG paths for generic runner inputs; no second parse or context cache key is added.
- `~/1/devtools/ymake/include_parsers/nlg_includes_parser.rl6` defines the import grammar. `include_processors/nlg_processor.cpp` consumes external imports as generated .pb.txt files, induces .h dependencies, and passes serialized imports to every output. The NLG draft registers this parser and threads parsed induced dependencies into existing generated-file registry entries.
- The approved EN boundary remains unchanged: `dev/bugs/20260711-upstream-en-generator-compile-closure-overemit.md` and `~/1/tools/enum_parser/parse_enum/parse_enum.cpp:391` show that enum generation reads only its direct header. Compilation owns C++ closures. The earlier claim of a comparator defect is retracted; comparison tooling is unchanged.

Preserved checkpoints: independent source-only4907a9e, earlier broad96abbdc, continuationc4a6456; selected implementation starts atb611e64. Reviewed selected025f0b7+88f1487 and YDB restorationb0830be are frozen on their own branches. NLG draftc59c7b2 is preserved separately and is not a completed acceptance checkpoint.

Canonical lint and all143 unit targets pass for b0830be. Its tiny branching COPY cycle fails before the fix when entered through two of its three members, then passes through every entry. A frozen88 diagnostic isolated two COPY inputs missing mkql_builtins_decimal.h.txt; the other42 node differences were UID/dependency cascades. Normalized evidence is retained and newly generated large raw graphs were removed.

Fresh targeted gate for b0830be: YDB generated successfully and is **OK27272/0/0** (binary8cd95cafd8dab04829bc59d1a46f52ea8c43c6356b0da14f3ec1408946ba550a). The frozen SWIG9b27251 Go gate is15772/268/268, improving the selected88 result by10 matches. NLG canonical lint and all144 unit targets pass, including the native whitespace/comment/import recovery tests. Its targeted Go/YDB comparison is pending; this is a unit-tested checkpoint, not full acceptance.

The following table is ROOT's actual full22 gate for88f1487, before b0830be. All cases generated and compared with sandboxing. This is historical baseline evidence, not a full-gate claim for the unfinished NLG stage. ROOT reported OK16/XFAIL5/FAIL1; YDB's44 differences are resolved by the separate fresh targeted gate above.

| Case | Phase | matched/our_only/ref_only | Status |
| --- | --- | --- | --- |
| arc_server_bin | compare | 12060/0/0 | OK |
| catboost_app | compare | 5963/0/0 | OK |
| devtools_ya_bin | compare | 14889/0/0 | OK |
| devtools_ya_bin_2 | compare | 16325/0/0 | OK |
| devtools_yamaker | compare | 9600/0/0 | OK |
| devtools_ymake_bin | compare | 8797/0/0 | OK |
| devtools_ymake_bin_2 | compare | 5780/0/0 | OK |
| p12_codegen_tail | compare | 35941/5368/10636 | XFAIL |
| p12_config | compare | 49739/95/722 | XFAIL |
| p12_cuda | compare | 2647/0/0 | OK |
| p12_docs | compare | 16/0/0 | OK |
| p12_go | compare | 15762/278/278 | XFAIL |
| p12_java | compare | 12451/0/0 | OK |
| p12_packaging | compare | 19679/3145/3214 | XFAIL |
| p12_proto | compare | 4778/0/0 | OK |
| p12_py2 | compare | 3680/0/0 | OK |
| p12_tests | compare | 24630/553/1176 | XFAIL |
| p12_typescript | compare | 1248/0/0 | OK |
| search_daemons_basesearch | compare | 21390/0/0 | OK |
| search_daemons_ranking_middlesearch | compare | 47104/0/0 | OK |
| yabs_server_daemons_bs_static | compare | 42854/0/0 | OK |
| ydb_apps_ydbd | compare | 27228/44/44 | FAIL |

Timing evidence is kept separate by stage. Independent source-only4907a9e versus99bece0: five warmed alternating p12_config samples, median1.512699s→1.523625s (+0.7223%), identical clean environment and sandbox flags. ROOT completed the separate3ff47a5 eleven-p12 measurement against99 with a largest slowdown of4.165%. Neither is a performance acceptance claim for the unfinished full NLG stage. Full-stage acceptance timings remain required; generation_seconds from an individual gate is not treated as a paired performance measurement.

Outstanding: complete NLG targeted validation; hand the frozen tested stage to ROOT for combined22 acceptance and timing; continue any remaining generic input differences. The global all22-green goal is not complete.
