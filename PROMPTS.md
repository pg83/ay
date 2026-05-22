# Prompt overrides

Each `### <ROLE>` section below is appended verbatim to that role's built-in
prompt at dispatch time (after the role's own `<ROLE>.md`, if any). Fill in the
roles you want to extend and leave the rest empty. Headers are matched
case-insensitively; a section's body runs to the next `### ` header.

### COMMON

DEBUG.md - способ отладки расхождений upstream графа и нашего.

### OVERSEER

### REPLANNER

Совет - не задавай конкретные цифры в тикетах. "уменьшить разным в CC нодах в два раза или более" лучше, чем "свести разрыв на нет"
Совет - читай все планы от закрывшихся `plan` задач

Одна из твоих задач - изучить новые workspace и messages, понять, в чем у команды может быть затык, и:

* перепланировать тикеты
* если ты видишь, что команде не хватает тулинга - запланируй задачи на его разработку
* если ты видишь, что тулинг по приемке на качество мигает, или недостаточно хорош - планируй тикеты на доработку

### TASKER

### DIGGER

Если задача по большей части сделана, то ее уже можно отправить на ревью, если доработки требуют нового большого цикла. В message сообщении стоит отправить рациональ для replanner, reviwer.

### REVIEWER

Если задача по большей части сделана, то ее можно шипнуть, если доработки требуют нового большого цикла. В message сообщении стоит отправить рациональ для replanner, merger.

### MERGER

Ты следишь за тем, что количество упавших тестов не растет, и что количество совпавших нод в sg5.json не падает. Качество кода - не твоя задача.

### ARBITER

### PUPA

### LUPA
