# RUN_PROGRAM: семантика prInputClosure и бакеты инклудов generated-файлов (2026-07-05)

Статус: **prInputClosure оставлен в green-виде — каждая его ветка эмпирически
пригвождена контрпримером из референса.** Модель инклудов generated-файлов
переведена на `ParsedIncludeSet` (local = собственные рёбра файла, cpp =
compile-добавка потребителям). Ночь калибровки v5–v7 и три абляции ниже — чтобы
не повторять.

## Модель (коммиты 3f61599, ece3c8e)

`GeneratedFileInfo.ParsedIncludes` — `ParsedIncludeSet`, как у source-парсеров:

- **local** — собственные include-рёбра файла в смысле ymake (то, что «висит» на
  файловом узле): у PR-выхода — OUTPUT_INCLUDES; у flatc `.fbs.h` — индуцированные
  заголовки импортов; у protoc `.pb.h` — **пусто**.
- **cpp** — добавка, которую видят только compile-обходы потребителей: у `.pb.h`
  — pb.h импортов + protobuf-extras + raw-`.proto` rel'ы; у PR-выхода —
  IN-carries + proto-import pb.h + mainHeaderInclude.

Сканер разворачивает build-файл как local+cpp — компиляции байт-идентичны.

## Доказанные факты про ymake (агенты по /home/pg/monorepo/yatool + референс)

1. RUN_PROGRAM — 100% конф-макрос (`ymake.core.conf:4781`), C++-спецкода нет.
2. JSON-inputs ноды: source-файл (`EMNT_File`) добавляет себя, inputs текут
   вверх сквозь всё, кроме Program/Library (`json_visitor.cpp:472,788`).
3. У protoc-`pb.h` **нет собственных рёбер**: PR с `OUTPUT_INCLUDES <generated
   pb.h>` без IN даёт inputs=[tool] (кейс `ads/argus/.../profile_traits.h`,
   ровно 1 input в референсе) — при том что `contrib/tools/protoc/
   ya.make.induced_deps` объявляет INDUCED_DEPS: они материализуются у
   потребителей, не на файле.

## Каждая ветка prInputClosure ← пригвождающий кейс

| ветка | кейс (bs_static/ydbd референс) |
|---|---|
| ранний nil (`INs==0 && !fullSourceClosure`) | `profile_traits.h`: OUT .h+.cpp, OI pb.h → inputs=[tool] |
| `fullSourceClosure` (INs==0 && main-CC) → OI-walk keep=isSource | `formula_parameters.cpp` (STDOUT): 1602 inputs, flatbuffers runtime+libcxx, $(B)-членов нет |
| fullSourceClosure walk header-OUT'ов | `query_params.h`: 1549 inputs = закрытие INDUCED_DEPS(h) собственного тула (через resolveInducedDeps в walk) |
| `selfScan = INs>0 && (parsedIN \|\| !genHeader)` | `bsyeti/libs/features`: IN `.in` (непарсимый) + OUT formula.cpp formula.h → inputs=[tool, formula.in]; devtools-кейсы дают обратное при parsedIN |
| IN-walk verbatim (включая $(B) pb.h) | `sys_const_transfer.cpp`: ref держит $(B)/*.pb.h из proto-IN закрытий |
| OI-walk keep=extIsProto + pb.h-сиблинги (при INs>0) | `ydb/core/control/lib/generated/control_board_proto.h`: 157 протосов через OI на generated pb.h — абляция ветки валит только ydbd |

## Фальсифицированные «упрощения» (не повторять без новой идеи)

- **v5**: рекурсия по local-рёбрам + ClosureLeaves + глобальная фаза после
  генерации. Провалы: leaves pb.h (=source proto) — не рёбра; глобальная фаза
  ломает SourceInputs-тайминг yapyc-потребителей (pass2 обязан остаться
  per-module); «только кэшированные children» не спасает host-инстансы
  (llvm16/include на host: потребителей нет в принципе).
- **v6** «walk всех/только CC выходов»: вторичные CC-выходы не текут
  (profile_traits.cpp), их узлы — собственность их компиляций.
- **v7** «течёт только STDOUT-main»: опровергнут devtools_ya_bin_2 (OUT-main-cpp
  течёт при parsedIN) — различитель именно green-условие selfScan.
- Резолв include в контексте модуля-генератора запрещён и не нужен: tablegen
  `llvm16/include` (без ADDINCL) не резолвит `#include "*GenRegisterInfo.inc"`
  (файл зарегистрирован в чужом каталоге) — walk-и green делают резолв в
  правильных модульных контекстах по построению.

## Итого

prInputClosure — минимальная эмпирически-точная кодировка ymake-поведения
PR-inputs; сокращать дальше можно только с новым знанием механики
ymake-пропагации (см. отчёты агентов в истории сессии).
