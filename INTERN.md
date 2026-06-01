# Параллелизация gen и барьер интернирования — журнал исследования

Дата: 2026-05-31. Бенчмарк везде — `sg5` gen (`ay make -j 0 --sandboxing ydb/apps/ydbd`,
без `-G`, серийный путь). Базовый wall ~2.4 s, RSS ~800 MB, serial CPU ~2.2 s.

Цель: найти структурный выигрыш по wall в build-graph генераторе `ay`.
Ресурсы неограничены — можно переписывать ядро, если есть понятный профит.

---

## 1. Где время: сканер include-замыканий

После фикса content-hash (отдельный коммит) горячая серийная точка — сканер
транзитивных include-замыканий: `emitOneSource → walkClosureRoot → WalkClosure →
dfsID → forEachResolvedChild` (~25% serial CPU). GC параллельный, off critical path.

### Как устроено замыкание

- Замыкание каждого файла предпосчитано **один раз** и лежит как упорядоченный
  `[]VFS`-window в плоской арене `subgraphChunks`; указатель — `subgraphCache[VFS] =
  closureRef{off,n}`. Циклы свёрнуты Тарьяном (`strongconnect`); все члены SCC делят
  один `ref`.
- `closureOf(abs)` возвращает **полное** транзитивное замыкание (leaf-ready), не
  частичную структуру: `strongconnect` собирает {члены SCC} ∪ {готовые
  `closureWindow` детей вне SCC}.
- `dfsID` для НЕ-source узла (заголовка) не рекурсит — высыпает готовое окно.
  Рекурсия (`plainDfsID`) только для `isSourceLike` (= `.cpp/.cc/.c/.S/...`).

### Как сливаются сиблинги (проверено по коду)

Ни попарно, ни пирамидкой. **Один общий `idSet`-аккумулятор, в который линейно
ссыпаются VFS всех сиблингов**, дедуп на лету:

```
acc := idSet{}            // общий (visited / tjClosure)
out := []VFS{}            // выход (order / buf)
for каждый сиблинг C:
    for id := range closureOf(C):   // линейный скан ПОЛНОГО окна C
        if acc.has(id) { continue } // уже положил предыдущий сиблинг → skip
        acc.add(id); out = append(out, id)
```

Два сайта одинаковой формы:
- `WalkClosure`/`dfsID`: сиблинги = прямые include исходника; `visited`/`order`;
  per-source, ~18k раз.
- `strongconnect`: сиблинги = дети всех членов SCC; `tjClosure`/`buf`; per-closure,
  кэшируется.

`idSet` — epoch-штампованный плотный массив на `vfsBound` (индекс = `uint32(v)`),
проба `has` ≈ 1.4 ns.

---

## 2. Замеренная избыточность слияния

Инструментировали цикл `dfsID`:

```
windowDumps = 113120        iter = 83.4M
hit (already-visited skip)  = 59.1M  (70.9%)   ← переобход перекрытий
add (новые)                 = 24.3M  (29.1%)
avg window = 737 элементов/dump
```

**71% всех 83M проб — это re-scan перекрывающихся замыканий соседних заголовков.**
Подтверждено: общие STL/util под-замыкания пересканируются. НО фактор избыточности
всего **3.45×** (iter = add/(1−0.71)), и per-op дёшев → весь дедуп ≈ 5–8% CPU,
избыточная часть ≈ 3–5% wall.

---

## 3. Что НЕ сработало (доведено до замера/расчёта)

### Roaring bitset-union (замерено — РЕГРЕСС)

Идея: хранить каждое замыкание как roaring-битсет, `WalkClosure` = OR битсетов
прямых инклудов. Прототип (`github.com/RoaringBitmap/roaring`, `GOSUMDB=off go get`):

- **Регресс: wall 2.4 → 3.1 s, RSS 800 → 900 MB.**
- Причина: замыкания **разрежены** в 32-битном id-пространстве (~700 файлов на 800k
  ids), roaring не доходит до bitmap-контейнеров — деградирует в sorted `[]uint16`
  array-контейнеры, где insert/union = binary-search + memmove
  (`iaddReturnMinimized` 15%, `binarySearch` 7.6%, `memmove` 5.6%). Медленнее
  cache-friendly плотного `idSet`.
- Byte-exact при этом сохранялся (gate сортирует inputs → порядок неважен).

Вывод по представлениям: roaring (разреженный) — медленнее; плотный `[]uint64`
на замыкание — настоящий word-OR, но 100 KB × 26k замыканий ≈ **2.6 GB**, нереально;
sorted-list merge — то же O(элементов), что сейчас. **Ни одно не бьёт `idSet`
element-walk на этих данных.**

### Idea 2 — closure-by-reference / DAG-шаринг (расчёт — проигрывает)

Хранить замыкание как {own SCC} + ссылки на замыкания детей (не сплющивать),
материализовать DFS по DAG-у замыканий с O(1)-пропуском посещённых узлов.

Расчёт по числам: стоимость = U + E (уникальные файлы + рёбра между ними). Выигрывает
только если средний include-fanout < 59M/24M ≈ **2.46**. Реальный C++ fanout 5–30 →
E ≫ 59M → **заметно медленнее**. Сплющивание окон — и есть оптимизация, меняющая
дорогой обход рёбер на дешёвый скан плоских списков; перекрытие 3.45× — её плата,
меньшая, чем повторный обход рёбер.

### Intra-merge параллелизм (замерено распределение — нечего дробить)

Распределение работы по walk'ам:

```
n=22795 walks   total=82.5M   max=286932   mean=3621
top 0.1% = 2.7%   top 1% = 9.1%   top 5% = 25.7%   top 10% = 40.0%   top100 = 5.6%
```

Нет жирной головы. mean walk ≈ 5 мкс (3621 × 1.4 ns) — **ниже overhead горутин**.
max walk ≈ 400 мкс (один). Параллелить можно только хвост >50 мкс — доли процента
walk'ов, <15% работы, от ~6%-пирога мерджа = <1% CPU. **Не окупается.**

**Итог по слиянию замыканий: локальный оптимум для этой формы данных.** Проверено
тремя способами (roaring-замер, DAG-расчёт, распределение).

---

## 4. Настоящий рычаг — крупнозернистый параллелизм across walks/modules

22.8k независимых walk'ов / тысячи модулей — отлично ложатся на пул воркеров (мелкое
зерно не проблема, когда задач тысячи). Но gen **однопоточный**. Что мешает:

- `subgraphCache`/`childrenCache` — шардируемы per scanner/ctx. ОК.
- `parsedIncludes` — можно под lock (толстое, но lock терпим). ОК.
- `sysinclSourceCache`/`searchTierCache` — шардируемы. ОК.
- **`internTable` — холдаут.** Append-only, lock-free, single-writer. Это **самая
  важная оптимизация**; глобальный лок убьёт (846k обращений сериализуются).
  → Параллелить можно только то, что **ничего не интернит** (всё уже заинтернено).

### Барьер: uid-проход intern-free, но мал; интернирование размазано

- uid-путь (`uid.go`, `emitter.go` finalize/`resolveAndUID`/`nodeUIDWithBuf`) —
  **ноль вызовов интерна** (проверено грепом). Это чистый параллельный остров, уже
  отделён как `finalizeNodesInOrder`. НО топологически зависим: uid ноды читает
  `uids[depRef.id]` → параллель волной по dep-DAG, не свободный fan-out. И мал (~10%).
- Интернирование: **167k вставок / 679k чтений** (20% записи), таблица → 180k.
  Размазано по всему gen.

---

## 5. Ключевой замер: scan-интерн vs emit-интерн

Тег фазы на `internString` (внутри `resolve()` = scan, иначе = emit):

```
intern miss = 166855
  scanMiss (resolve, источники)  =  27240  (16.3%)
  emitMiss (output/cmd/codegen)  = 139615  (83.7%)   ← !!!
```

**Переворот гипотезы.** Интернирует в основном ЭМИССИЯ (84%, 140k строк —
output-пути `$(B)/…`, cmd-токены, codegen-выходы), а не сканер (27k, шарятся через кэш).

Следствия:
1. «Freeze-after-scan + parallel-lookup-emit» — **мёртв** (эмиссии надо вставить 140k).
2. **Local-intern → serial re-intern (deferred canonicalization) — единственный приём,
   ложащийся на факт.** Подтверждена исходная идея PG, а не «заморозка».
3. Сканер можно не трогать (интернит мало, держит шаринг).

---

## 6. Выкристаллизовавшаяся архитектура (кандидат)

```
1. Serial scan (как сейчас): resolve + closures, ~27k интернов, общие кэши.
   → замороженная глобальная база id.

2. Parallel emit (P воркеров, work-stealing по модулям):
   - читают замороженную базу + кэши по ГЛОБАЛЬНЫМ id (только чтение, без лока);
   - новые строки (140k) интернят в ЛОКАЛЬНУЮ таблицу воркера (тегированный
     id-диапазон), глобальную не трогают;
   - тяжёлый compute: composeTargetCC, EmitCC, cmd-args.

3. Serial canonicalize (re-intern по сохранённым local-id):
   union локальных таблиц → глобальная; remap[localID→globalID]; переписать id в
   нодах. ~140k hash-insert + обход полей нод — дёшево относительно п.2.

4. Parallel uid: топо-волной по dep-DAG (intern-free, read-only).
```

Параллелится **эмиссия (~65%) + uid**; глобальный `internTable` в параллельных фазах
только читается (инвариант PG соблюдён). Локальный интерн точечно — к 140k строк
эмиссии, почти не пересекающимся между модулями (свой output-каталог).

Запасной/упрощённый вариант: серийно пред-интернировать output-пути (выводимы из
структуры модулей), тогда часть эмиссии — pure-lookup без remap. Но 140k emit-интернов
частично закрытие-производны → полный local+remap надёжнее.

### Твёрдые места

- **Детерминизм NodeRef** при параллельной эмиссии: id-диапазоны на модуль в
  детерминированном порядке модулей, не в порядке готовности воркеров.
- **Кросс-модульный dep-DAG** для фазы uid.
- **Исчерпывающий remap** всех полей с id (Inputs/Outputs/Deps/cmd-токены).
- **Расщепление streaming → scan-фаза + emit-фаза**: для `-G`/validate чистый выигрыш;
  для реального билда теряется ранний старт executor (придётся батчить).

---

## 7. Разбор emit-интернов (замерено) — remap дёшев

По сайтам (emit-промахи ~140k; `Source`/`Build`/`Intern` все воронкой через
`internString`, классификация по префиксу):

```
raw    (internString без $()-префикса)  = 67813  (49%)   — цели инклудов, cmd-токены
build  ($(B)/…)                          = 48369  (35%)   — output-пути + codegen-выходы
source ($(S)/…)                          = 23433  (17%)   — source-пути, интернённые в эмиссии
intern$(                                 =     0
```

codegen механически не отделить — он внутри `build`-бакета ($(B)/ генерённые выходы).

Межмодульное пересечение emit-строк (главное для стоимости remap):

```
touched distinct = 162207
single-owner         = 156054  (96.2%)   ← интернит ровно ОДИН модуль
cross-module-shared =    6153  (3.8%)     ← касаются >=2 модулей
```

**96.2% emit-строк строго module-local** → при параллельной эмиссии с per-worker
локальным интерном 96% строк не коллидируют между воркерами. Serial canonicalize =
hash-merge локальных таблиц в глобальную; 3.8% общих (6k) схлопываются в один
глобальный id естественно (hash-insert дедуплицирует). **Стоимость remap = ~140k
hash-insert + обход id-полей нод — дешёвый хвост, НЕ узкое место.**

Deferred-canonicalization подтверждён по обеим осям: emit интернит много (84%) →
нужен local-intern (freeze не годится); emit-строки почти module-local (96%) →
канонизация партиционируется и дёшева.

## 7a. ЖЁСТКОЕ ОГРАНИЧЕНИЕ: streaming fast-start в `make` нарушать нельзя

В реальном билде (`make.go`): `go ex.eventLoop()` стартует executor ДО `genStream`,
а `genStream(..., ex.onNode, ...)` пихает каждую ноду в executor по мере эмиссии →
**exec перекрывается с продолжающейся генерацией** (fast-start). uid считается inline
в `StreamingEmitter.Emit`, сразу за ним `onNode`.

Следствие: **batch-parallel-gen (буфер всех нод → параллельный emit/uid) сериализует
gen→exec и убивает перекрытие.** Поэтому весь parallel-gen (включая параллельный uid)
— только для **буферизованного пути** (`-G`/validate/CI/профиль), а реальный билд
остаётся streaming. Для билда это не потеря: серийная стоимость gen там скрыта за exec.

В коде разделение уже есть: `StreamingEmitter` (inline uid, fast-start, build) vs
`BufferedEmitter` (`finalizeNodesInOrder`, batch). parallel-gen вешается ТОЛЬКО на
`BufferedEmitter`; `StreamingEmitter.Emit` не трогаем. Риска для билда нет — это
физически другой путь кода. Область выигрыша parallel-gen = скорость
graph-generation, НЕ wall полевого билда.

## 8. Следующий риск — НЕ remap

Узкое место архитектуры теперь не интернирование/remap (доказанно дёшевы), а:
- **детерминизм NodeRef** при параллельной эмиссии (id-диапазоны на модуль в
  детерминированном порядке модулей);
- **кросс-модульный dep-DAG** для топо-волны uid (фаза 4);
- **расщепление streaming → scan-фаза + emit-фаза** (теряется ранний старт executor
  для реального билда; для `-G`/validate — чистый выигрыш).

Следующий шаг: прототип одной фазы для замера реального speedup — либо параллельный
uid-проход (безопасный, ~10%, по dep-DAG), либо сразу скелет scan→parallel-emit→
canonicalize на `-G`-пути (большой приз ~65%, но с риском детерминизма NodeRef).

---

## Принципы, подтверждённые по ходу

- Не верить гипотезам — мерить (гипотеза «scan интернит больше» оказалась обратной).
- Порядок inputs неважен для gate (нормализация сортирует) — это факт, принят.
- `internTable` неприкосновенен (lock-free single-writer) — самая важная оптимизация;
  параллелим только уже-заинтернённое.
- Мир больше готового кода: local-intern + deferred canonicalization — валидный приём,
  не ограниченный текущей single-table streaming-архитектурой.

## Map probing — полный разбор по source location (после DenseMap для CodegenRegistry)

Профиль `ay make -j 0 ydb/apps/ydbd`. Перечислены ВСЕ map-entry-points рантайма
(`faststr`/`fast32`/`fast64`/generic; generic пуст) → это 100% map-работы (~20% CPU на
clean-ране ~2.0s), не топ-N. Инлайн-хэш-внутренности (`matchH2`, `h2`, `aeshashbody`,
`memhash32`, `getWithoutKeySmallFastStr`) — это общий probe-механизм, уже включён в cum
каждого сайта. Доли — относительно суммарного map-probing.

1. Intern table — `internTable.ids map[string]STR` · ~35% (самый большой).
   String-keyed; зовётся из `internString` + `internBytes` + `interned`.
   ```go
   // vfs.go:34
   func internString(s string) STR {
       if id, ok := internTable.ids[s]; ok { return id }   // ← hot
       ...
       internTable.ids[s] = id                              // ← assign (+rehash)
   ```
   DenseMap? Нет — строковый ключ. Неустранимый "string → id" gate.

2. FS dir cache — `osFS.dirs map[string]map[string]bool` · ~20%.
   Два faststr-probe на запрос (`Listdir` → потом `Exists`).
   ```go
   // fs.go Listdir:  if cached, ok := fs.dirs[rel]; ok { return cached }   // 23%
   // fs.go Exists:   entries := fs.Listdir(dir); isDir, ok := entries[name] // 12%
   ```
   DenseMap? Нет (строковые ключи), НО можно ключевать по интернённому STR директории →
   два faststr-probe станут fast32/массивом. Самый перспективный остаток.

3. `searchTierCache map[STR]searchTierResult` · ~15% (крупнейший fast32).
   ```go
   // scanner.go:1179 resolveContextSearchTier
   if cached, ok := sc.searchTierCache[targetID]; ok { s.searchTierHits++; return cached }
   ```
   DenseMap? Ключ плотный (STR), НО per-scanCtx (много короткоживущих) → vfsBound idx-массив
   на каждый scanCtx дороже выигрыша. (hit ~89% по прежней заметке.)

4. `Environment map[string]string` (macros) · ~8%. `SetBool`/`SetString`/`Get` при сборе модуля.
   `// macros.go:122  func (e Environment) SetBool(...) { e[name] = ... }`
   DenseMap? Нет — строковые ключи, короткоживущий.

5. Parser include set — `rawParsedIncludeSet` (parser_manager.go:21) · ~6%. Строковый дедуп при парсе. Нет.

6. `sourceClassCache map[string]uint32` (scanner.go:558) · ~5%. Строковый ключ (source rel). Нет.

7. Tarjan/peer closures (fast32) — `strongconnect.func1`, `genModule.func2/func5/func12`,
   `forEachResolvedChildID` · ~7%. Per-walk scratch по VFS (`genModule.func5` — крупнейший assign).
   DenseMap? Нет — per-walk scratch; сюда просится epoch-stamped `idSet`, не DenseMap.

8. `unionIncluderMappings` (sysincl header, string) ~3% + `sourceClassBuckets map[uint64][]uint32`
   (`sysinclSourceLookup`, fast64) ~1%. Нет — string / uint64-hash ключи.

Сумма ≈ 100% → хвоста нет, эти 8 сайтов и есть весь map-probing. Состав:
- ~70% string-keyed (intern, FS dirs, Environment, parser, sourceClassCache, sysincl headers) →
  DenseMap НЕ применим (нужен плотный целочисленный ключ).
- ~15% (`searchTierCache`) — плотный ключ, но per-scanCtx (не та lifetime).
- ~7% (Tarjan/peer) — просит `idSet`, не DenseMap.

Вывод: CodegenRegistry был единственным DenseMap-образным map'ом. Дальнейшие рычаги:
(1) intern table (35%) — классический string→int hash; обогнать встроенный swiss-map Go 1.24
    можно только спец-интернером (open-addressing + кэш xxh3, интерн по []byte) — большой проект,
    выигрыш неочевиден; (2) FS dir cache (20%) — ключевать по STR директории (пути уже интернятся),
    два faststr-probe → fast32 → массив — самый осуществимый остаток; (3) остальное (~30%) — мелочь,
    строковое или короткоживущее.
