# MUSL Refactoring Plan

## Goal

Убрать `musl` из ядра графогенератора `yatool` и привести его к
upstream-модели:

- ядро (gen / scanner / emitters) о musl ничего не знает;
- `make.go` принимает `--musl` и складывает `MUSL=yes` в платформенный flag bag;
- consumer-policy (`_BASE_UNIT`, `_BASE_PROGRAM`, sysincl-routing) живёт в
  data tables, читаемых движком как набор правил;
- module-local musl shape (CFLAGS / ADDINCL / NO_PLATFORM / NO_RUNTIME)
  лежит в parsed `moduleData`, как любое другое `ya.make`-наследие.

Final state:

| Слой | Знание musl | Источник |
| --- | --- | --- |
| CLI / orchestrator | флаг `--musl` ⇒ `MUSL=yes` в env | upstream `ya.musl` parity |
| `Platform.Flags["MUSL"]` | bool-shadow ради быстрого `when`-eval | persistence только |
| rule tables | declarative `when ($MUSL == "yes")` rows | mirror `ymake.core.conf` + `sysincl.conf` |
| `moduleData` | `addIncl`, `cFlags`, `flags.NoStdInc`, `noPlatform`, … | parsed `contrib/libs/musl/ya.make` |
| sysincl YAMLs | filenames, content | unchanged (data) |
| ядро Go-кода | none | refactor target |

## 1. Upstream Model (deepened)

### 1.1 `devtools/ymake/` (C++ graph engine) — ZERO musl

`grep -ri musl /home/pg/monorepo/yatool_orig/devtools/ymake/` → пусто.
Это эталон: ядро `ymake` не содержит ни `MUSL`, ни `musl`-токена.
Вся policy приходит **извне** через два механизма:

1. **`MUSL`-env-var** — bag `(name, value)`, выставленный orchestrator-ом.
2. **`build/*.conf`** — declarative DSL с `when (...) { ... }`-блоками,
   которые движок интерпретирует.

### 1.2 `devtools/ya/` orchestrator — узкий surface

- CLI option:
  [`build/build_opts/__init__.py:392`](/home/pg/monorepo/yatool_orig/devtools/ya/build/build_opts/__init__.py)
  ```python
  ['--musl'], help='Build with musl-libc',
  hook=SetConstValueHook('musl', True)
  ```
- env propagation:
  [`build/graph.py:2579`](/home/pg/monorepo/yatool_orig/devtools/ya/build/graph.py)
  ```python
  if opts.musl: yield 'MUSL', 'yes'
  ```

Дальше ya не делает ничего musl-специфичного. `MUSL=yes` просто попадает
в global var-bag перед evaluation conf-файлов.

### 1.3 `build/ymake.core.conf` — 7 declarative когда-блоков

Все production-policy сосредоточена в одном файле:

| Строка | Контекст | Эффект |
| --- | --- | --- |
| `348` | `_BASE_UNIT` `NORUNTIME != yes` | `SYSINCL += libc-musl-libcxx.yml` |
| `356` | `HARDENING == yes && CLANG` | `_C_FLAGS += -fstack-protector -D_hardening_enabled_` (без `-all`) |
| `409` | global allocator default | `DEFAULT_ALLOCATOR=TCMALLOC_TC` |
| `781` | `_BASE_UNIT` | `CFLAGS += -D_musl_`, `PEERDIR += contrib/libs/musl/include` |
| `954` | tcmalloc/lf gate | `MUSL != yes && WITH_VALGRIND == yes` |
| `1230` | `_BASE_PROGRAM` asmlib branch | `MUSL != yes && OS_LINUX && X86_64 && !SAN && SSE4` → glibcasm; иначе asmlib |
| `1238` | `_BASE_PROGRAM` | `MUSL_LITE` → `PEERDIR += contrib/libs/musl`; иначе `musl/full` |
| `4381` | `NO_LIBC()` macro | `DISABLE(MUSL)` |

Дополнительно:

- [`build/conf/sysincl.conf:51`](/home/pg/monorepo/yatool_orig/build/conf/sysincl.conf) — sysincl YAML routing (`libc-to-musl.yml` + arch-specific);
- [`build/conf/go.conf:1725,1978`](/home/pg/monorepo/yatool_orig/build/conf/go.conf) — Go-specific PEERDIR `contrib/libs/musl/full`.

Везде это data, не control flow.

### 1.4 `build/ymake_conf.py` — config GENERATOR, не graph generator

Здесь musl есть, но это Python-скрипт, который **один раз** генерирует
тулчейн-конфиг до того, как ymake начнёт строить граф:

- [`ymake_conf.py:1431`](/home/pg/monorepo/yatool_orig/build/ymake_conf.py) — `setup_sdk` → `--sysroot=/nowhere` под MUSL.
- [`ymake_conf.py:1778`](/home/pg/monorepo/yatool_orig/build/ymake_conf.py) — `self.musl = Setting('MUSL', convert=to_bool)`.
- [`ymake_conf.py:1803-1804`](/home/pg/monorepo/yatool_orig/build/ymake_conf.py) — `-Wl,--no-as-needed` под MUSL.
- [`ymake_conf.py:2547`](/home/pg/monorepo/yatool_orig/build/ymake_conf.py) — `auto_have_cuda` → False под MUSL.

Граница: `ymake_conf.py` принадлежит "tooling/setup", а не "graph
engine". Аналог в `yatool` — это могла бы быть отдельная stage
`buildToolchainConfig(flags) → Toolchain`, читаемая ядром как чёрный
ящик. Сейчас этой границы нет: `yatool` смешивает «настройка тулчейна»
и «генерация графа» в одном Go-коде.

### 1.5 `contrib/libs/musl/ya.make` — module-local data

Целиком эталонные значения для musl-self compile:

```
NO_COMPILER_WARNINGS()
NO_PLATFORM()
NO_RUNTIME()
ADDINCL(
    arch/x86_64 | arch/aarch64,
    arch/generic, src/include, src/internal, include, extra,
)
CFLAGS(
    GLOBAL -D_musl_=1
    -D_XOPEN_SOURCE=700 -U_GNU_SOURCE
    -nostdinc -ffreestanding -fno-stack-protector
    -D__libc_calloc=calloc -D__libc_malloc=malloc -D__libc_free=free
)
```

`contrib/libs/musl/include/ya.make` экспортит `GLOBAL ADDINCL` consumer-half.

В upstream-модели **никакого** `if musl { ... }` для применения этих
значений не нужно: они уже есть в module attributes, и compile pipeline
применяет их как любые другие `CFLAGS`/`ADDINCL`/`NO_PLATFORM` бит.

## 2. Current Leaks in `yatool` (categorised)

Полный учёт. Категория = тип утечки, не серьёзность.

### Cat A — orchestrator surface (UNAVOIDABLE)

Допустимый surface, mirrors upstream `--musl` → `MUSL=yes`:

- [`make.go:2083`](make.go), [`:2134`](make.go), [`:2151`](make.go),
  [`:2713`](make.go), [`:2742`](make.go), [`:2845`](make.go) —
  `--musl` opt, `mf.musl bool`, `targetFlags["MUSL"]="yes"`.

### Cat B — module-data parsing (UNAVOIDABLE)

- [`modules.go:55`](modules.go) `muslLite`
- [`modules.go:56`](modules.go) `muslEnabled`
- [`modules.go:225`](modules.go) `env.Bool("MUSL")`
- [`modules.go:507`](modules.go) `IF(MUSL)` eval
- [`modules.go:812-821`](modules.go) `ENABLE(MUSL_LITE)` parse
- [`parsers.go:3395`](parsers.go), [`:3412`](parsers.go)
  parse-time MUSL probes

Эти места читают `ya.make` / env. Имя поля `muslEnabled` — допустимо;
поведение data-driven.

### Cat C — Platform-flag persistence (UNAVOIDABLE)

- [`platform.go:1719`](platform.go) `Flags map[string]string` хранит `"MUSL"="yes"`.
- [`platform.go:1731`](platform.go) `LibcMusl bool` — shadow того же бита.
- [`platform.go:1809`](platform.go) `LibcMusl: flags["MUSL"] == "yes"`.

Канонический storage CLI-флага. `LibcMusl bool` — мини-leak: даёт
прямой `if p.LibcMusl` вместо `if p.Flags["MUSL"] == "yes"` и тем
самым приклеивает name "musl" к Platform API. Phase 1 кандидат на снос.

### Cat D — consumer policy в `defaults.go` (Phase 3)

Прямой перенос policy из `ymake.core.conf:781, :1238, :409`:

- [`defaults.go:33`](defaults.go) `runtimeAncestorPaths["contrib/libs/musl"]=true`
- [`defaults.go:250-251`](defaults.go) `if !noPlatform && !NoStdInc && muslOn → peers += musl/include`
- [`defaults.go:301-302`](defaults.go) program-path копия того же правила
- [`defaults.go:312-347`](defaults.go) `effectiveMuslOn` / `cliMuslEnabled` predicates
- [`defaults.go:357-369`](defaults.go) `consumerCFlags` → `[muslConsumerSentinel]`
- [`defaults.go:383-386`](defaults.go) `const muslConsumerSentinel = "-D_musl_"`
- [`defaults.go:406-460`](defaults.go) `defaultProgramPeerdirsForWithState`:
  MUSL_LITE → `musl`, !MUSL_LITE → `musl/full`, MUSL=yes+linux → TCMALLOC_TC

### Cat E — musl-self bundles, зашитые в `flags.go` (Phase 4)

- [`flags.go:316-318`](flags.go) `muslWarningFlags = ["-Wno-everything"]`
  — это `NO_COMPILER_WARNINGS` из `contrib/libs/musl/ya.make:25`.
- [`flags.go:349-359`](flags.go) `muslExtraDefines = [9 args]` — это
  буквальный `CFLAGS(...)` блок из `contrib/libs/musl/ya.make:51-61`.

Должны жить в parsed `moduleData.cFlags` / `moduleData.cFlagsGlobal`,
а compile pipeline должен применять их как любые другие модульные CFLAGS.

### Cat F — emitter control flow с musl-dispatch (Phase 4 follow-up)

- [`emit_cc.go:204`](emit_cc.go) `isMusl := instance.Flags.NoStdInc`
- [`emit_cc.go:267-270`](emit_cc.go) `case isMusl && isHost / isMusl` → разные composers
- [`emit_cc.go:896-944`](emit_cc.go) `composeMuslCC` / `composeMuslHostCC`
- [`emit_as_helpers.go:1230-1275`](emit_as_helpers.go) тот же dispatch в AS
- [`emit_ld.go:382`](emit_ld.go), [`:431`](emit_ld.go) explicit
  `if muslOn { append(autoPeerCFlags, muslConsumerSentinel) }`
- [`emit_bi.go:1088`](emit_bi.go) литерал `"-D_musl_"`
- [`emit_dynamic_library.go:683`](emit_dynamic_library.go) передаёт
  `effectiveMuslOn(ctx, d)` в `composeLDCmdVcsCompile`
- [`emit_py_proto.go:493`](emit_py_proto.go) `AutoPeerCFlags: [muslConsumerSentinel, "-DUSE_PYTHON3"]`

Этот слой целиком должен исчезнуть после Cat D/E: emitter получает
готовые `CFlags`/`Warning`/`AutoPeerCFlags` слоты, наполненные данными,
и не делает musl-dispatch.

### Cat G — sysincl YAML routing в `sysincl.go` (Phase 5)

- [`sysincl.go:421-501`](sysincl.go) `linuxMuslSysInclOrder` + `LoadSysInclSetFor(arch)`.

Это порт `sysincl.conf:51`. Сейчас YAML-имена в Go-коде; должны быть в
declarative rule table или прямо в конфиг-файле.

### Cat H — env-default table (Phase 3 sibling)

- [`macros.go:3211`](macros.go) `MUSL: true` (canary env default)
- [`macros.go:3230`](macros.go) `MUSL_LITE: false`

Эти строки сами по себе — data, но они живут в Go-таблице defaults
вместо отдельного toolchain-config файла.

### Cat I — path-rewriting shim в `sources.go` (Phase 6)

- [`sources.go:142-157`](sources.go) `jsTargetPeerAddIncl`:
  rebases `contrib/libs/musl/arch/<from>` → `contrib/libs/musl/arch/<to>`
  на JS-closure-scan, потому что JS-нода якорится на target axis, а
  окружающий walk — host axis.

Самая «грязная» утечка: helper, который текстово знает «musl/arch» и
переписывает путь. Решение — корректная host/target propagation, без
musl-knowledge.

### Cat J — комментарии-документация (НЕ leak)

Большое число `musl`-вхождений в `.go` файлах — это упоминания в
комментариях, документирующие M2/M3 reference behaviour. Их трогать
не нужно; они описывают, как ведёт себя система, а не задают её.

## 3. Target Architecture

Три data layer'а + libc-agnostic engine.

### 3.1 `Platform` — target-level facts only

Допустимое:

- `Flags map[string]string` (хранит `MUSL=yes`/`MUSL_LITE=yes` etc.)
- derived `LibcFlavor string` (= `"glibc" | "musl"`) ОПЦИОНАЛЬНО,
  если упростит rule predicates; иначе предикаты читают `Flags["MUSL"]`
- `Triple`, `March`, `Tools`, `OS`, `ISA`, `IsHost`, `PIC` — уже есть

Запрещено:

- `MuslExtraDefines []string` — это module data, не platform
- `MuslAddIncl []VFS` — то же
- любой ad-hoc `bool` shadow для отдельных env-bits сверх минимума

Дискуссия: оставлять ли `LibcMusl bool`. Аргумент за — короткий predicate
в hot-paths. Аргумент против — он живёт ровно один раз сейчас (для
sysincl arch-выбора и пары `if p.LibcMusl`-сайтов), и его легко выразить
через `p.Flags["MUSL"] == "yes"`. Phase 1 предложение: удалить.

### 3.2 `moduleData` — module-local facts

После Cat E переноса должно полностью описывать musl-self shape:

```go
type moduleData struct {
    addIncl        []string  // local ADDINCL (musl-self arch + generic + …)
    addInclGlobal  []string  // GLOBAL ADDINCL (musl/include consumer)
    cFlags         []string  // local CFLAGS (musl-self -nostdinc, ...)
    cFlagsGlobal   []string  // GLOBAL CFLAGS (-D_musl_=1)
    flags struct {
        NoStdInc            bool
        NoPlatform          bool
        NoRuntime           bool
        NoCompilerWarnings  bool
    }
    muslEnabled  bool
    muslLite     bool
    hadAllocator bool
    allocatorName string
    ldPlugins    []string
    // …
}
```

Принципиально: emitter, получив `moduleData`, должен применять эти
поля **унифицированно**. `NoStdInc=true` УЖЕ задаёт `-nostdinc` поведение
(уже работает в `flags.go` → `includeScannerBasePaths`). Осталось
сделать так же для CFLAGS-набора: вместо `if isMusl: muslExtraDefines`
должно быть `cmdArgs += d.cFlags`.

### 3.3 Rule tables — mirrored conf policy

Один engine, две таблицы:

```go
// implicitPeerRule mirrors `_BASE_UNIT` / `_BASE_PROGRAM` peer rules.
type implicitPeerRule struct {
    name      string                // for tracing
    phase     phasePhase            // unit | programPre | programPost
    predicate func(*ruleCtx) bool   // closed over Platform.Flags + moduleData
    peer      string                // path to inject
    suppress  func(*ruleCtx) bool   // self-guard + subtree-suppress (NO_PLATFORM, runtime-ancestor, etc.)
}

// autoCFlagRule mirrors `_BASE_UNIT` `CFLAGS += -D_musl_` family.
type autoCFlagRule struct {
    name      string
    predicate func(*ruleCtx) bool
    flag      string
}
```

`ruleCtx` = `(ModuleInstance, *moduleData, *Platform)`. Engine — единый
loop, без `if musl ...`. Musl упоминается только в data:

```go
var unitImplicitPeers = []implicitPeerRule{
    {name: "musl/include", phase: phaseUnit,
     predicate: flagEq("MUSL", "yes").and(notNoPlatform).and(notNoStdInc),
     peer:      "contrib/libs/musl/include",
     suppress:  inRuntimeAncestor("contrib/libs/musl")},
    // …
}

var autoCFlags = []autoCFlagRule{
    {name: "consumer-musl", predicate: flagEq("MUSL", "yes").and(notNoPlatform),
     flag: "-D_musl_"},
    // …
}
```

Этот же engine читает таблицу для `_BASE_PROGRAM` (musl vs musl/full),
для allocator-default и т.д.

## 4. Migration Stages

Порядок — от низкого риска к высокому, с byte-exact gate'ом на каждом
шаге. Ниже у каждой стадии: scope / steps / acceptance / risk.

### Execution Status

| Phase | Status | Commit | Notes |
| --- | --- | --- | --- |
| 0 — Freeze | partial | (existing `dev/validate.sh`) | M2+M3 sg2 byte-exact gate exists; explicit characterization tests deferred |
| 1 — `LibcMusl` snos | **done** | `aef9914` | dead shadow removed |
| 2 — auto-peer CFlag funnel | **done** | `c28ee6a` | `consumerAutoPeerCFlags` helper; emit_ld + emit_py_proto routed through it |
| 3 — implicit peer rule tables | **done** | `074409f` | `implicitPeerRule` + 3 tables (unit/program/allocator) |
| 4 — musl-self compile data → moduleData | pending | — | high risk; needs Phase 0 explicit tests for `composeMuslCC{,Host}` byte-pin |
| 5 — sysincl YAML routing → table | **done** | `e0e7105` | `sysInclYamlSequence []sysInclEntry` |
| 6 — host/target propagation | pending | — | `jsTargetPeerAddIncl` shim removal |
| 7 — cleanup + grep guard | pending | — | run after 4+6 |

Byte-exact M2 (sg2.aarch64) + M3 (sg2.x86_64) preserved on every commit
above; sg3.aarch64 pre-existing diff (first byte 61971) is unchanged.

### Phase 0 — Freeze (PREREQUISITE)

**Scope**: characterization tests + perf gate, чтобы любая стадия имела
жёсткий regression guard.

**Steps**:

1. зафиксировать M2 + M3 + (если есть) sg2 sha256 в `validate.sh`;
2. добавить characterization tests:
   - `defaults_test`: для `defaultPeerdirs*`, `consumerCFlags`,
     `defaultProgramPeerdirs*` со всеми комбинациями
     (NO_PLATFORM, NoStdInc, MUSL, MUSL_LITE, hadAllocator) — таблица
     ожидаемых результатов;
   - `emit_cc_musl_test`: композеры `composeMuslCC` / `composeMuslHostCC`
     закреплены byte-exact;
   - `emit_ld_test`: `composeLDCmdVcsCompile{,Host}` под MUSL=yes/no;
   - `py3_proto_test`: cmd[1] cmp pin.
3. вписать в `validate.sh` `go test ./...` + `perf 5s` gate.

**Acceptance**: чистый `go test ./...`; `validate.sh` passes;
M2+M3 sha pinned.

**Risk**: 0. Только тесты, без логики.

### Phase 1 — Прибрать Platform API (`LibcMusl` → `Flags["MUSL"]`)

**Scope**: убрать `Platform.LibcMusl` shadow.

**Steps**:

1. grep всех `p.LibcMusl` / `.LibcMusl`;
2. заменить на `p.Flags["MUSL"] == "yes"` (либо helper `p.flagYes("MUSL")`);
3. удалить поле и его init в `platform.go:1731,1809`.

**Acceptance**: M2+M3 byte-exact; `go test ./...`; sha не меняется.

**Risk**: низкий. Чисто рефакторинг.

### Phase 2 — Unify auto-peer-CFlag injection

**Scope**: убрать ручную сборку `[muslConsumerSentinel, ...]` в
`emit_ld.go:380-383, 429-432` и `emit_py_proto.go:493`.

**Steps**:

1. ввести `autoCFlagsFor(instance, d, platform) []string` в `defaults.go`,
   читающий **одну** таблицу `autoCFlags` (Cat D + Phase 4 предтечa);
   на этом этапе таблица содержит только текущую логику
   `consumerCFlags`, но callable из любого emitter;
2. заменить в `emit_ld.go`/`emit_py_proto.go` ручную сборку на
   `autoCFlagsFor(...)`;
3. чтобы не сломать M2, gate `autoCFlagsFor` ровно теми же предикатами,
   что были inline.

**Acceptance**: M2+M3 byte-exact; emitter-side musl-string literals
исчезают вне `defaults.go`.

**Risk**: средний — predicate parity.

### Phase 3 — Implicit peers в rule table

**Scope**: `defaults.go:234-470` (`defaultPeerdirsForWithState`,
`defaultProgramPeerdirsForWithState`, `effectiveMuslOn`,
`muslConsumerSentinel`).

**Steps**:

1. ввести `implicitPeerRule` + engine; заполнить таблицу строго по
   текущему поведению (musl/include unit + musl/musl/full program + allocator);
2. оставить функции `defaultPeerdirs*` тонкими wrapper'ами над engine;
3. убедиться, что MUSL_LITE branch выражен как два правила (musl без full
   и `peer-of-peer`-ность остаётся);
4. удалить `if muslOn { ... }` ветки.

**Acceptance**: M2+M3 byte-exact; defaults_test зелёный без изменений;
`defaults.go` не содержит `musl`-литералов вне `var unitImplicitPeers`
и `var programImplicitPeers`.

**Risk**: высокий — это самый большой policy-перенос. Mitigation:
characterization test из Phase 0 + малыми кусками (unit-rules → program-rules → allocator).

### Phase 4 — Musl-self compile data → `moduleData`

**Scope**: `flags.go:311-359` (`muslWarningFlags`, `muslExtraDefines`)
и dispatch в `emit_cc.go:204,267-270,896-944` + аналог в
`emit_as_helpers.go:1230-1275`.

**Steps**:

1. parsed `contrib/libs/musl/ya.make` должен заполнить
   `d.cFlagsGlobal = ["-D_musl_=1"]` и `d.cFlags = [-D_XOPEN_SOURCE=700,
   -U_GNU_SOURCE, -nostdinc, -ffreestanding, -fno-stack-protector,
   -D__libc_calloc=calloc, -D__libc_malloc=malloc, -D__libc_free=free]`;
2. в parser `NO_COMPILER_WARNINGS()` уже даёт `d.flags.NoCompilerWarnings`;
   compile pipeline должен читать его и в этом случае давать `[-Wno-everything]`
   вместо стандартного warning-bundle. Это убирает `muslWarningFlags`
   как отдельную сущность;
3. убрать `composeMuslCC` / `composeMuslHostCC`; одна generic
   `composeCC` берёт CFLAGS из `d.cFlags + d.cFlagsGlobal`;
4. убрать `isMusl := NoStdInc` dispatch — `NoStdInc` сам по себе уже
   задаёт `-nostdinc` (через `includeScannerBasePaths`) и `-nostdinc`-блок
   в CFLAGS (через `d.cFlags`);
5. удалить `muslWarningFlags`, `muslExtraDefines`, `composeMuslCC`,
   `composeMuslHostCC` из `flags.go`/`emit_cc.go`/`emit_as_helpers.go`.

**Acceptance**: M2+M3 byte-exact; `flags.go` не содержит `musl`-литералов;
`emit_cc.go` / `emit_as_helpers.go` не делают `isMusl`-dispatch.

**Risk**: высокий. Параллельно затрагивает CC + AS. Mitigation:
Phase 0 byte-exact pins на `composeMuslCC`/`composeMuslHostCC` + порядок
аргументов в `appendCompileFlagPipeline` под микроскопом.

### Phase 5 — sysincl YAML routing → declarative

**Scope**: `sysincl.go:421-501` (`linuxMuslSysInclOrder`, `LoadSysInclSetFor`).

**Steps**:

1. описать routing как таблицу `(predicate, yamlFile)` — mirror
   `sysincl.conf:51`;
2. `LoadSysInclSetFor` обходит таблицу, не switch'ит на arch инлайново;
3. в идеале — читать `build/conf/sysincl.conf` напрямую (это data),
   но это отдельный uplift; на первом проходе достаточно переноса в
   таблицу Go.

**Acceptance**: M2+M3 byte-exact; `sysincl.go` не содержит хардкода
`linux-musl-aarch64.yml` / `linux-musl.yml` в Go switch'е (только
в таблице).

**Risk**: средний. Существует memory note "sysincl YAMLs runtime-parsed
exception" — Phase 5 затрагивает routing, не сами YAML'ы.

### Phase 6 — host/target propagation (убрать `jsTargetPeerAddIncl`)

**Scope**: `sources.go:142-157`.

**Steps**:

1. понять, почему JS-closure нуждается в target-axis musl-arch:
   потому что walk'аем host-axis, но JS-node якорится на target.
   Правильный фикс — closure-scan под target-axis scanner вместо
   rebase-host-входа;
2. ввести `scanClosureFor(target Platform, ...)` либо передавать
   `PeerAddInclGlobal` уже target-axis с upstream (там, где он
   собирается);
3. удалить `jsTargetPeerAddIncl`.

**Acceptance**: M2+M3 byte-exact; `sources.go` не содержит `musl`-литерала.

**Risk**: средний — host/target propagation шире, чем JS-only fix.

### Phase 7 — Cleanup и success-criteria pass

**Scope**: финальная зачистка comment-references, `Platform.Flags`-helper,
консолидация `effectiveMuslOn` (если что-то осталось).

**Steps**:

1. оставшиеся `cliMuslEnabled` / `effectiveMuslOn` — переименовать в
   `flagYes("MUSL")` либо удалить;
2. `runtimeAncestorPaths` — генерится из rule table, не литерально;
3. comment-pass: упоминания musl в комментариях оставить, если они
   документируют поведение; убрать, если они описывают **отсутствующий**
   код.

**Acceptance**:
- `grep -ri 'musl' yatool/*.go | grep -v _test.go | grep -v '^.*://'`
  даёт только: `make.go` (CLI), `modules.go` (parse), `platform.go`
  (Flags storage), `defaults.go` (rule table data), `sysincl.go` (rule
  table data), `macros.go` (env default table). НЕТ упоминаний в
  `emit_*.go`, `flags.go`, `scanner.go`, `gen.go`, `sources.go`.

## 5. First Safe Code Step

**Phase 1 целиком** — самый дешёвый и низко-рисковый старт:

1. Найти `LibcMusl`:
   ```
   grep -n 'LibcMusl' *.go
   ```
2. Заменить на `Flags["MUSL"] == "yes"` или ввести helper
   `func (p *Platform) Flag(k string) string`.
3. Удалить поле + init в `platform.go`.
4. Прогнать:
   ```
   go test ./...
   ./validate.sh
   ./yatool gen --target devtools/ymake/bin … | sha256sum
   ```
5. Закоммитить.

Дальше уже Phase 2.

## 6. What NOT to Do

- НЕ переносить `muslExtraDefines` в `Platform` — это module data, а не
  platform fact. Один musl-target может иметь несколько `ya.make`-ов
  с разным NO_PLATFORM/NO_RUNTIME профилем.
- НЕ сливать consumer `-D_musl_` (no `=1`) и musl-self `-D_musl_=1` в
  один источник. Это разные семантики: первый — `_BASE_UNIT` для
  всех consumer'ов, второй — GLOBAL CFLAGS из `contrib/libs/musl/ya.make`.
- НЕ делать `helperFor(musl)` без перехода ownership-слоя. Helper, который
  «прячет musl behind a function name», но всё ещё знает строки «musl»,
  не считается отрефакторенным.
- НЕ трогать sysincl YAML файлы. Они data, и hand-translate их в Go
  таблицу запрещено (memory note: 11k-line resolution tables).
- НЕ объединять Phase 3 и Phase 4 в один commit. Они независимы по
  surface и оба рискованны; merging уменьшает testability.

## 7. Success Criteria

Работа завершена, когда:

1. **Grep test**: `grep -ri 'musl' yatool/*.go | grep -v _test.go`
   даёт упоминания **только** в:
   - `make.go` (CLI flag)
   - `modules.go` + `parsers.go` (parsed module attributes)
   - `platform.go` (`Flags["MUSL"]` storage)
   - `defaults.go` (rule table data)
   - `sysincl.go` (rule table data)
   - `macros.go` (env default table)
   Любое появление в `emit_*.go`, `flags.go`, `scanner.go`, `gen.go`,
   `sources.go` — фейл.
2. **Byte-exact**: M2 (`tools/archiver`) и M3 (`devtools/ymake/bin`)
   sha256-пины из `validate.sh` совпадают на каждой стадии.
3. **Tests**: `go test ./...` зелёный.
4. **Perf**: `./yatool gen --target tools/archiver` укладывается в 5 с
   acceptance gate.
5. **Architectural test**: добавить `MUSL`-новой policy
   (например, `MUSL=yes && OS_LINUX → -D_NEW_FLAG`) делается
   добавлением строки в rule table, без изменения engine-кода.

Последний критерий — самый честный: refactor закончен, когда новая
policy добавляется в **data**, а не в **code**.

## 8. Verification Recipe

Один shell-блок, который проверяет всё на любой стадии:

```bash
# 1. compile
go build ./... || exit 1

# 2. tests
go test ./... || exit 1

# 3. M2 byte-exact
./validate.sh || exit 1

# 4. M3 byte-exact
./yatool gen --target devtools/ymake/bin \
    --target-platform default-linux-aarch64 \
    --host-platform default-linux-x86_64 \
    --python-bin /ix/realm/pg/bin/python3 \
    --c-compiler /ix/realm/boot/bin/clang \
    --cxx-compiler /ix/realm/boot/bin/clang++ \
    --objcopy /ix/realm/boot/bin/llvm-objcopy \
    --host-platform-flag MUSL=yes \
  | sha256sum | grep -q c735f57761bf265292cc1ebd97a022f15290cb1fd31db3128762c880b7beb6f4 \
  || { echo "M3 sha mismatch"; exit 1; }

# 5. perf
test "$(( $(date +%s%N) ))" # …
./yatool gen --target tools/archiver --target-platform default-linux-aarch64 \
    --host-platform default-linux-x86_64 \
    --python-bin /ix/realm/pg/bin/python3 \
    --c-compiler /ix/realm/boot/bin/clang \
    --cxx-compiler /ix/realm/boot/bin/clang++ \
    --objcopy /ix/realm/boot/bin/llvm-objcopy \
    --host-platform-flag MUSL=yes >/dev/null
# (real timing wrapper — perf gate ≤5 s)

# 6. grep guard
ALLOWED='make.go|modules.go|parsers.go|platform.go|defaults.go|sysincl.go|macros.go'
FOUND=$(grep -rln 'musl\|MUSL' *.go | grep -v _test.go | grep -Ev "^($ALLOWED)$")
if [ -n "$FOUND" ]; then
  echo "FAIL: musl leaked into: $FOUND"
  exit 1
fi
```
