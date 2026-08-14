# 开放知识格式（OKF）

**版本 0.2**

OKF 是一种开放、对人类与智能体（agent）友好的格式，用于表示*知识*：即围绕数据与系统的元数据、上下文和经过整理的洞见。它既可由人类编写，也可由智能体生成，可跨组织交换，且能被双方共同消费。

该格式刻意保持极简：一个由带 YAML frontmatter 的 markdown 文件组成的目录。没有 schema 注册中心，没有中央权威，也不依赖任何必需的工具。如果你能 `cat` 一个文件，你就能读 OKF；如果你能 `git clone` 一个仓库，你就能发布它。

本文档自成一体：它规定了产出与消费 OKF v0.2 所需的一切。相对于 v0.1 的变更摘要见 §13。

---

## 1. 动机

面向 AI 智能体的知识表示领域演进迅速，许多互不兼容的约定正在涌现。OKF 的立场是：知识最好以普遍可访问、成熟的格式来表示，这些格式应当：

- **可读**：无需工具，人类即可阅读。
- **可解析**：无需专用 SDK，智能体即可解析。
- **可 Diff**：在版本控制中可进行差异比较。
- **可移植**：可跨工具、跨组织、跨时间使用。

知识语料库越来越不是"写一次再反复读"，而是**由智能体持续编写与维护**。当大多数概念由机器生成时，消费者需要得到一些普通"markdown + frontmatter"约定并未将其作为一等公民来对待的答案：

1. 它是从什么创建的，又是如何被验证的？（**溯源 provenance**）
2. 我该在多大程度上信任它？（**信任 trust**）
3. 它现在还成立吗？（**时效性 freshness**）
4. 它是当前版本吗？（**生命周期 lifecycle**）
5. 这个数字是否按我们规定的方式产出？（**证明 attestation**）

OKF v0.2 将溯源、信任、生命周期和证明提升为一等公民，同时保持格式的最小化主张。该格式本身是克制的，它只标准化那一小部分使知识语料库具备自描述能力的结构性约定——除此之外一切都交由生产者决定。

### 目标

1. 定义一种通用格式，供**生产者**（人、智能体、导出流水线）写入。
2. 指导**消费者**（智能体、UI、搜索索引、确定性代码）应如何读取与遍历它。
3. 促进知识跨系统、跨组织的**交换**。
4. 标准化那一小部分使智能体维护的语料库**可被信任**的 frontmatter 字段，且不规定任何运行时。

### 非目标

- 定义一套固定的概念类型分类法。
- 规定存储、服务或查询基础设施。
- 取代领域特定的 schema（Avro、Protobuf、OpenAPI 等）。OKF *引用*它们，而非吞并它们。
- 为执行器（executor）或证明器（attester）所指向的代码规定打包或调用标准。OKF 固定接口，不固定打包方式。

---

## 2. 术语

- **知识束（Knowledge Bundle，简称 bundle）**：一个自包含的、层次化的知识文档集合。它是分发的单位。
- **概念（Concept）**：束中知识的单个单元，表现为一份 markdown 文档。它可描述一个有形资产（一张表、一个 API）、一个抽象概念（一个指标、一个业务流程），或介于两者之间的任何事物。
- **概念 ID（Concept ID）**：概念文件在束中的路径，去掉 `.md` 后缀。
- **Frontmatter**：位于 markdown 文件顶部、以 `---` 界定的 YAML 元数据块。
- **正文（Body）**：文件中 frontmatter 之后的所有内容。
- **链接（Link）**：从一个概念指向另一个概念的标准 markdown 链接，用于表达隐式父子层次之外的更丰富关系。
- **来源（Source）**：概念所派生自的材料，可在束外或束内，记录于 `sources` frontmatter 字段。
- **溯源（Provenance）**：概念所派生自的全部来源的集合。
- **可信度信号（Credibility signal）**：每个来源客观的、逐来源的事实（`author`、`usage_count`、`last_modified`），用于推断信任；OKF 记录信号，而非裁决结论（见 §5.1）。
- **行动者（Actor）**：标识谁（或什么）执行了某动作的字符串，约定如下：智能体用 `<producer>/<version>`，人用 `human:<id>`，自动化流程用 `process:<id>`（见 §7）。
- **信任级别（Trust tier）**：由概念的 `verified` 字段派生出的等级：未验证、机器确认、或人工复核（见 §5.3）。
- **证明计算（Attested Computation）**：一种概念（`type: Attested Computation`），携带着计算某个值的受认可方式，使消费者能确认该值是通过运行它而产生的（见 §10）。
- **执行器（Executor）**：运行指令或代码，执行一次计算并返回凭证（receipt）（见 §10.2）。
- **凭证（Receipt）**：一次运行返回的证据，其结构由 `executor.receipt` 决定；这是运行时产物，不存储在束中（见 §10）。
- **证明器（Attester）**：确定性（无 LLM）代码，检查一份凭证并返回裁决（见 §10.2）。

---

## 3. 束结构

一个束是一棵 markdown 文件的目录树。目录结构与领域无关：生产者按所捕获知识的合理性来组织概念。

```
path/to/bundle/
  index.md                      # 可选。目录清单，用于渐进式披露。
  log.md                        # 可选。按时间顺序的更新历史。
  <concept>.md                  # 位于束根的一个概念。
  <subdirectory>/               # 子目录将概念分组。
    index.md
    <concept>.md
    <subdirectory>/
      ...
```

一个束**可以（MAY）**以如下方式分发：

- 一个 git 仓库（推荐，因为它提供历史、归属和 diff）。
- 该目录的 tarball 或 zip 归档。
- 一个更大仓库中的子目录。

### 3.1 保留文件名

以下文件名在层次结构的任何层级都有确定含义，且**不得（MUST NOT）**用于概念文档：

| 文件名     | 用途                          |
|------------|----------------------------------|
| `index.md` | 目录清单。见 §8。       |
| `log.md`   | 更新历史。见 §9。          |

所有其他 `.md` 文件都是概念文档。

标签通过 `tags` frontmatter 字段（§4.1）保持为一等公民。OKF 不为按标签聚合文档指定单独的文件格式；想要标签浏览视图的消费者可在消费时扫描 frontmatter 自行合成一个。

---

## 4. 概念文档

每个概念都是一个 UTF-8 markdown 文件，包含两部分：

1. 一个 **YAML frontmatter 块**，以单独一行的 `---` 开头，并以单独一行的 `---` 结束。
2. 一个 **markdown 正文**，包含自由形式的内容。

### 4.1 Frontmatter

```yaml
---
type: <类型名称>                  # 必填
title: <可选的显示名称>
description: <可选的单行摘要>
resource: <底层资产的可选规范 URI>
tags: [<tag>, <tag>, ...]          # 可选
# ... 信任、生命周期、溯源与计算族（见 §5、§10）
# ... 其他生产者自定义的键值对
---
```

**必填：**

- `type`：标识概念种类的短字符串。消费者用它做路由、过滤和展示。示例值：
  `BigQuery Table`、`BigQuery Dataset`、`API Endpoint`、`Metric`、
  `Playbook`、`Reference`、`Attested Computation`。

  类型值**不**集中注册。生产者**应当（SHOULD）**选择描述性强、自解释的值；消费者**必须（MUST）**优雅地容忍未知类型，通常将其作为通用概念处理。

`type` 是唯一始终必填的键；只带 `type` 的概念即完全合规（§11）。

**推荐：**

- `title`：人类可读的显示名称。若省略，消费者**可以（MAY）**从文件名派生标题。
- `description`：概括概念的单句。供 `index.md` 生成器、搜索摘要和预览使用。
- `resource`：唯一标识概念所描述底层资产的 URI。对于描述抽象概念而非物理资源的概念，此字段缺省。
- `tags`：用于横切分类的短字符串 YAML 列表。

可选的**溯源**、**信任**和**生命周期**族（§5），以及 Attested Computation 概念的**计算**字段（§10）也可能出现。

**扩展：** 生产者**可以（MAY）**包含任何附加键。消费者在往返处理时**应当（SHOULD）**保留未知键，且**不得（MUST NOT）**因字段无法识别而拒绝文档。

### 4.2 正文

正文是标准 markdown。生产者**应当（SHOULD）**优先使用结构化 markdown（标题、列表、表格、围栏代码块）而非自由散文，因为结构既利于人类阅读，也利于智能体检索。

正文没有必需的小节。以下标题具有**约定俗成**的含义，适用时**应当（SHOULD）**使用：

| 标题           | 用途                                                |
|----------------|--------------------------------------------------------|
| `# Schema`     | 资产列/字段的结构化描述。   |
| `# Examples`   | 具体用法示例，通常为围栏代码块。  |
| `# Computation` | Attested Computation 的受认可计算。见 §10。 |

对外部来源的逐论断归因使用以 `sources` 条目为键的 markdown 脚注，而非正文中的引文列表（§5.1）。

### 4.3 示例：绑定到资源的概念

```markdown
---
type: BigQuery Table
title: Customer Orders
description: One row per completed customer order across all channels.
resource: https://console.cloud.google.com/bigquery?p=acme&d=sales&t=orders
tags: [sales, orders, revenue]
generated: { by: reference_agent/gemini-2.5-pro, at: 2026-05-28T14:30:00Z }
---

# Schema

| Column        | Type      | Description                              |
|---------------|-----------|------------------------------------------|
| `order_id`    | STRING    | Globally unique order identifier.        |
| `customer_id` | STRING    | Foreign key into [customers](/tables/customers.md). |
| `total_usd`   | NUMERIC   | Order total in US dollars.               |
| `placed_at`   | TIMESTAMP | When the customer submitted the order.   |

# Joins

Joined with [customers](/tables/customers.md) on `customer_id`.
```

### 4.4 示例：未绑定到资源的概念

```markdown
---
type: Playbook
title: "Incident response: data freshness alert"
description: Steps to triage a freshness alert on the orders pipeline.
tags: [oncall, incident]
generated: { by: human:ahormati, at: 2026-04-12T09:00:00Z }
---

# Trigger

A freshness alert fires when `orders` lags more than 30 minutes behind its
expected SLA. See the [orders table](/tables/orders.md).

# Steps

1. Check the [ingestion job dashboard](https://example.com/dash).
2. ...
```

---

## 5. 溯源、信任与生命周期

这些 frontmatter 族使"它从哪来""我该多信任它""它是否仍最新"这些问题可由 frontmatter 回答。它们全部可选。它们的缺失带有含义：一个未验证的概念与一个已验证的概念可区分，但绝不会因此被拒绝（§11）。

### 5.1 溯源：`sources`

`sources` 记录概念所派生自的材料，可在束外或束内。

```yaml
sources:
  - id: ga4-schema
    resource: https://developers.google.com/analytics/bigquery/export-schema
    title: GA4 BigQuery Export schema
    author: team:ga4-docs
    usage_count: 5000
    last_modified: 2026-05-30
usage_window: { from: 2026-06-01, to: 2026-06-30 }
```

每个 `sources` 条目：

- `resource`：条目内**必填**。要么命名一个消费者可追随的具体产物（绝对 URL、束相对路径，或 `references/` 子目录中的路径，§6），要么命名一个它无法追随的群体或范围描述符（例如 `all queries in BigQuery project X`）。
- `id`：可选。一个稳定键，用于为单个论断归因（见下文）。当正文引用该来源时**应当（SHOULD）**出现。
- `title`：可选。来源的人类可读标签。
- 可选的可信度信号 `author`、`usage_count` 和 `last_modified`，下文说明。

**来源可信度信号。** OKF 记录客观的、逐来源的信号，使消费者能通过评判提取概念的来源来评判对概念的信任程度。它不存储可信度评分：评分是主观的、不可跨消费者移植、且会过时。可信度是从信号*推断*出来的，正如信任级别那样（§5.3），而非存储。每个信号都可选，位于 `sources` 条目上：

- `author`：谁或什么生产了该来源，采用行动者约定（§7）。一种权威性信号。
- `usage_count`：在 `usage_window` 内 `resource` 被使用的次数（仪表盘查看、查询执行、页面读取）。一种采用度与活跃度信号。对于单个产物，它是该产物自身的使用计数；对于范围描述符，它是该范围内触及概念的使用次数。
- `last_modified`：来源自身上次变更的时间（`YYYY-MM-DD`）。一种时效性信号，区别于 `generated.at`（§5.2），后者记录概念被编写的时间。
- `usage_window`：作为 `sources` 的同级字段写一次，以 `{ from, to }` 日期范围为每个 `usage_count` 提供上下文。单个条目**可以（MAY）**携带自己的 `usage_window` 以覆盖共享的那个。

`usage_count` 是一个粗粒度信号。它在"活 vs 死"和数量级层面、以及对照某来源自身历史趋势时是可比的，但不能作为精确的跨种类排名：一个计划查询的执行次数与一个人刻意的仪表盘查看并不等权重。消费者**应当（SHOULD）**把它读作活跃度与趋势，而非评分。

血缘（lineage）通过链接表达，而非专门字段。当 `resource` 指向另一个 OKF 概念时，派生边已存在于束图（§6）中，因此消费者**可以（MAY）**递归进入该来源自身的 `sources` 并让可信度传播。外部叶子来源只携带其固有信号。更深层的血缘（显式的外部 `derived_from`，或数据血缘）在 v0.2 中不涉及。

**逐论断归因。** 要为某个具体论断归因，使用一个 markdown 脚注，其标签为某个 `sources[].id`：

```markdown
The `events_` table is sharded daily as `events_YYYYMMDD`.[^ga4-schema]

[^ga4-schema]: GA4 BigQuery Export schema
```

脚注标签是进入 `sources` 的连接键；消费者通过匹配条目来解析归因，而非解析脚注文字。标签采用键名而非位置（`sources[0]`），因为智能体会不断改写这些文档：位置索引在列表重排的瞬间会静默错配，而稳定的 `id` 能在重排中存活。

### 5.2 信任：`generated` 与 `verified`

`generated` 记录当前内容是如何产出的。`verified` 记录谁或什么已对照其来源或 `resource` 确认了内容。二者保持区分，因为*编写*概念者未必是*确认*它的人。

```yaml
generated: { by: reference_agent/gemini-2.5-pro, at: 2026-06-20T22:53:05Z }
```

- `generated.by`：`generated` 内**必填**。一个行动者（§7）。
- `generated.at`：标记内容最后一次有意义变更的 ISO 8601 日期时间。消费者用它区分近期编辑与过时事实。

```yaml
verified:
  - { by: human:ahormati, at: 2026-06-25T09:00:00Z }
  - { by: process:finance-nightly, at: 2026-06-26T02:00:00Z }
```

- `verified`：验证事件列表，每个含 `by`（一个行动者）和 `at`（一个 ISO 8601 日期时间）。多个条目捕获独立检查，例如人工签字加夜间流程。"多近"以最新 `at` 为准。
- `verified` 独立于 `generated.at`：内容可变更而无需重新确认，事实也可重新确认而无需重新生成。
- 单个验证者**可以（MAY）**写成不含列表短划线的一个 `{ by, at }` 映射。消费者**必须（MUST）**把裸映射视为单元素列表：

```yaml
verified: { by: human:ahormati, at: 2026-06-25T09:00:00Z }
```

### 5.3 信任级别

消费者从 `verified` 派生信任级别，由低到高：

- 无 `verified` 键 ⇒ **未验证（unverified）**。
- `verified` 仅由非 `human:` 行动者做出 ⇒ **机器确认（machine-confirmed）**。
- `verified` 由 `human:<id>` 行动者做出 ⇒ **人工复核（human-reviewed）**。

没有信任 frontmatter 的概念仍可被消费；消费者**不得（MUST NOT）**拒绝它（§11）。信任级别是建议性信号，不是访问控制。

### 5.4 生命周期：`status`

```yaml
status: stable        # draft | stable | deprecated
```

- `draft`：尚未复核；可能不完整。
- `stable`：默认；可供消费。
- `deprecated`：为链接和历史保留；不再当前。

缺省 `status` ⇒ `stable`。

### 5.5 生命周期：`stale_after`

```yaml
stale_after: 2026-09-23   # 绝对日期；内容在此日及之后过时
```

可选。一个绝对日期（`YYYY-MM-DD`）。当 `今天 >= stale_after` 时概念过时。采用绝对日期而非相对 TTL，使过时判定成为纯粹的日期比较，无需参考概念被读取的时间。

---

## 6. 交叉链接与路径

### 6.1 概念之间的链接

概念**可以（MAY）**使用标准 markdown 链接指向其他概念。支持两种形式：

- **绝对（束相对）：** 以 `/` 开头，相对于束根解释。这是**推荐**形式，因为当文档在其子目录内移动时它保持稳定。

  ```markdown
  See the [customers table](/tables/customers.md) for the join key.
  ```

- **相对：** 标准 markdown 相对路径。

  ```markdown
  See the [neighboring concept](./other.md).
  ```

从概念 A 到概念 B 的链接断言一种*关系*。具体种类（父子、引用、关联、依赖）由周围文字传达，而非链接本身。构建图视图的消费者通常把所有链接当作无类型关系的有向边。

消费者**必须（MUST）**容忍断链：目标在束中不存在的链接并非格式错误；它可能只是表示尚未编写的知识。

### 6.2 取值为路径的字段

若干字段命名一个路径或 URI：`resource`、`sources[].resource`、`computation`、`executor.resource`、`attester.resource`（§10）。一个 `sources[].resource` 也可以是范围描述符（§5.1），此时它不是路径。每个取值为路径的字段接受：

- 一个绝对 URL（例如 `https://...`），
- 一个以 `/` 开头的束相对路径，或
- 一个相对路径（例如 `../computations/revenue.md`）。

### 6.3 `references/` 约定

`references/` 子目录按惯例把外部材料、运行指令或代码镜像为束内的一等概念。来源、执行器和证明器通常指向其中（例如 `references/attesters/revenue.py`）。这是一种命名约定，而非要求。

---

## 7. 行动者约定

记录身份的字段（`generated.by`、`verified[].by`）使用统一的行动者约定：

- 智能体和工具用 `<producer>/<version>`，例如 `reference_agent/gemini-2.5-pro`。
- 人用 `human:<id>`，例如 `human:ahormati`。
- 自动化流程用 `process:<id>`，例如 `process:finance-nightly`。

对信任分类的消费者（§5.3）以 `human:` 前缀为键，因此生产者**必须（MUST）**对人工编写或人工确认的内容使用它。

---

## 8. 索引文件

一个 `index.md` 文件**可以（MAY）**出现在任何目录，包括束根。它枚举该目录的内容，以支持**渐进式披露**：让人类或智能体在打开单个文档之前就能看到有哪些可用。

索引文件不含 frontmatter，但有一个例外：束根的 `index.md` **可以（MAY）**携带一个 `okf_version` 键（§12）。正文使用一个或多个小节，每个在标题下对概念分组：

```markdown
# 小节 / 分组标题

* [标题 1](relative-url-1) - 条目 1 的简短描述
* [标题 2](relative-url-2) - 条目 2 的简短描述

# 另一个小节

* [子目录](subdir/) - 子目录的简短描述
```

条目**应当（SHOULD）**包含所链接概念 frontmatter 中的 description。生产者**可以（MAY）**自动生成 `index.md`；消费者在缺失时**可以（MAY）**即时合成一个。

---

## 9. 日志文件

一个 `log.md` 文件**可以（MAY）**出现在层次结构的任何层级，以记录该范围的变化历史。格式为按日期分组的扁平条目列表，最新在前：

```markdown
# Directory Update Log

## 2026-05-22
* **Update**: Added a BigQuery table reference for [Customer Metrics](/tables/customer-metrics.md).
* **Creation**: Established the [Dataplex Playbook](/playbooks/dataplex.md).

## 2026-05-15
* **Initialization**: Created foundational directory structure.
```

日期标题**必须（MUST）**使用 ISO 8601 `YYYY-MM-DD` 形式。日志条目是散文；开头的粗体词（`**Update**`、`**Creation**`、`**Deprecation**`）是约定，非要求。

---

## 10. 证明计算概念

一个 Attested Computation 概念不仅携带一个值*意味着*什么，还携带一种受认可的*计算*方式，使消费者能确认智能体运行的是被认可的计算，而非自行即兴发挥。溯源（§5.1）回答"这个论断从哪来"；证明回答"这个数字是否按我们规定的方式产出"。OKF 记录计算与检查手段；它本身不执行任何东西。

### 10.1 计算本身是一个独立概念

一个受认可的计算是一个 `type: Attested Computation` 的独立概念。需要该值的概念（一个 `Metric`、一个 `BigQuery Table`）用普通 markdown 链接指向它（§6）。三个属性促使它成为独立概念：

- **`runtime` 定义了 `parameters` 的含义。** 一个参数根据 runtime 的不同，可能是 SQL 绑定变量、dbt var 或 Python 参数。把 `runtime` 与 `parameters` 放在同一个 frontmatter 中使绑定语义不言自明。
- **一个计算，多个消费者。** 同一计算可支撑一个指标、一个仪表盘概念和一份报告；作为概念，它被引用一次并被复用。
- **信任状态是逐计算的。** `verified`、`stale_after` 和单个 `attester` 描述的是一件事。收入、利润和利润率各自独立验证和证明，这是三个概念，而非一个 frontmatter 中的三个条目。

### 10.2 契约字段

契约是概念的顶层 frontmatter。除溯源、信任和生命周期族（§5）外，一个 Attested Computation 概念还携带：

- `runtime`：此类型**必填**。唯一说明如何运行计算的字段，因而决定执行器和证明器如何解释它、以及 `parameters` 的含义。示例值：`bigquery`、`postgres`、`dbt`、`python`、`Looker`。
- `parameters`：智能体可填入的、有类型的命名空位列表。每个条目：`{ name, type, required }`。绑定语义遵循 `runtime`。
- `computation`：可选。一个指向持有该计算的文件的路径（§6.2），用以替代内联正文围栏（见 §10.3）。缺省 ⇒ 正文 `# Computation` 围栏即为计算。
- `executor`：计算如何运行。`resource` 命名运行指令或代码；运行器（一个智能体，或确定性消费者代码）追随它。`receipt` 声明一次运行必须返回的字段，即证明器检查的证据（例如一个 BigQuery `job_id` 与该作业实际执行的 SQL）。
- `attester`：确定性检查。`resource` 命名代码（无 LLM），接收一份凭证并返回裁决。它设计为在消费者侧运行。

`resource` 背后是什么（一个 Skill、一个脚本、一个容器）是打包选择；OKF 固定接口，不固定打包（§1）。

```markdown
---
type: Attested Computation
title: Revenue for fiscal year
description: Recognized revenue for a fiscal year, per Finance's definition.
status: stable
runtime: bigquery
parameters:
  - { name: year, type: integer, required: true }
executor:
  resource: references/skills/run-on-bq.md
  receipt: [job_id, executed_sql, result]
attester:
  resource: references/attesters/revenue.py
generated: { by: reference_agent/gemini-2.5-pro, at: 2026-06-20T22:53:05Z }
verified: { by: human:ahormati, at: 2026-06-25T09:00:00Z }
stale_after: 2026-09-23
sources:
  - id: rev-policy
    resource: https://wiki.acme/finance/revenue-recognition
    title: Revenue recognition policy
---

# Computation

    SELECT SUM(amount) AS revenue
    FROM finance.recognized_revenue
    WHERE fiscal_year = @year

The computation binds only the declared `parameters`, per the recognition
policy.[^rev-policy]

[^rev-policy]: Revenue recognition policy
```

### 10.3 计算

用以下两种方式之一提供计算：

- **内联：** 正文 `# Computation` 下的单个围栏代码块。适合与契约一起复核的短计算。
- **文件：** 将 `computation` 设为路径（§6.2），省略正文围栏。适合较长或生成的计算，或已作为真实文件与非 OKF 工具共享的计算。

```yaml
runtime: bigquery
computation: references/computations/lib/revenue.sql
parameters:
  - { name: year, type: integer, required: true }
```

智能体**只可以（MAY）**为已声明的 `parameters` 提供*值*；它**不得（MUST NOT）**编写或编辑计算。把 `computation` 与参数值绑定成可执行产物是消费者的事，而证明器独立地重新派生同一绑定以与实际运行内容比较。因为比较发生在凭证所携带的展开后、编译后的产物上（`executed_sql`、`compiled_sql`），所以被改写的查询、被替换的计算文件或被篡改的依赖都会导致检查失败。一个有类型的、仅参数的表面正是让"是否运行了受认可的内容"成为机械比较而非主观判断的关键。

### 10.4 使用计算的概念

一份文档很少是单个计算。一份讨论收入、利润和利润率的损益表概览仍然是一个可读概念，并为每个数字链接到一个 Attested Computation：

```markdown
---
type: Metric
title: Revenue
description: Recognized revenue for a fiscal year.
tags: [finance, revenue]
status: stable
generated: { by: reference_agent/gemini-2.5-pro, at: 2026-06-20T22:53:05Z }
---

# Definition

Recognized revenue sums `amount` over rows booked to the fiscal year,
computed by [the revenue computation](../computations/revenue.md).
```

因为每个计算本身是一个概念，收入可以是新鲜的，而利润可能已过其 `stale_after`，且各自在各自运行上证明。把它们放在一起是目录选择（一个带 `index.md` 的 `computations/` 文件夹），而非 frontmatter 选择。

### 10.5 消费者如何使用它（资料性）

本小节是资料性（informative）而非规范性（normative）。下述运行时产物**不**存储在束中。

1. **发现**：通过 `type: Attested Computation`，一个可提升入 `index.md` 的 frontmatter 信号；消费者直接到达一个，或从使用它的概念跟随链接到达。
2. **加载**：从 frontmatter 加载契约，从正文（或 `computation` 命名的文件）加载计算。
3. **参数化**：智能体为已声明参数提供值。
4. **执行**：执行器运行绑定后的计算并返回一个由 `executor.receipt` 定形的凭证。
5. **证明**：消费者对凭证运行证明器。它确认溯源（运行的计算等于用所声明参数绑定的 `computation`，而非智能体自撰的 SQL）与保真度（显示的值与凭证的权威来源匹配，按作业 id 重新读取而非取自智能体文本）。
6. **门控**：拒绝展示一个失败的证明；当 `今天 >= stale_after` 时警告或拒绝。成功时，展示裁决（例如指向作业日志的链接）以使信任可见。

### 10.6 验证与证明

`verified`（§5.2）与证明是不同的，且二者并存：

- `verified` 确认*定义*仍符合政策。它是文档级的、慢的，记录在束中。
- 证明确认单次*运行*以受认可方式产出了该值。它是逐调用的、运行时的，不存储在束中。

一个定义过时的概念仍可干净地通过证明，而一个刚被验证的定义在每次运行时仍需证明，这正是二者都需要的理由。

---

## 11. 合规性

一个束在满足以下条件时与 OKF v0.2 **合规**：

1. 树中每个非保留的 `.md` 文件包含可解析的 YAML frontmatter 块。
2. 每个 frontmatter 块包含非空的 `type` 字段。
3. 每个保留文件名（`index.md`、`log.md`）在出现时分别遵循 §8 和 §9 的结构。

当信任、生命周期、溯源或计算族出现时，生产者**应当（SHOULD）**遵循 §5 至 §10，而消费者：

- **必须（MUST）**把裸 `verified` 映射视为单元素列表（§5.2）。
- **不得（MUST NOT）**因缺失任何可选族而拒绝概念（§5.3）。
- **应当（SHOULD）**仅从本文规定的字段派生信任级别与过时性，并**应当（SHOULD）**展示而非静默丢弃一个失败的证明（§10.5）。

消费者**应当（SHOULD）**将所有其他约束视为软性指导。特别地，消费者**不得（MUST NOT）**因以下原因拒绝一个束：

- 缺失可选 frontmatter 字段。
- 未知 `type` 值。
- 未知的附加 frontmatter 键。
- 断链。
- 缺失 `index.md` 文件。

---

## 12. 版本控制

本文档规定 OKF 版本 **0.2**。修订以 `<major>.<minor>` 版本化：

- **次版本**升级引入向后兼容的新增（新的可选字段、新的约定小节标题）。
- **主版本**升级可做出破坏性变更（重命名必填字段、更改保留文件名）。

束**可以（MAY）**用束根 `index.md` 的 frontmatter 块中的 `okf_version: "0.2"` 声明其目标版本（这是 `index.md` 中唯一允许 frontmatter 的地方）。不理解所声明版本的消费者**应当（SHOULD）**尽力消费，而非拒绝该束。

### 已考虑并推迟的事项

以下各项被有意留给未来修订：

- 完整的运行时协议：凭证与裁决的线路格式，以及围绕一次运行的证明生命周期。
- 证明器的 ABI、可移植性与沙箱化，很可能与服务和 Skills 的未来工作一并打包。
- 证明缓存。
- 语义层模板（Looker、dbt），其中证明器比较从 SQL 相等性转向模型与绑定相等性。

---

## 13. 相对于 v0.1 的变更

v0.2 取代 OKF v0.1，在 §12 下属次版本升级，但有两处刻意的破坏性变更，因它们重命名或弃用了 v0.1 字段而在此单独列出。一个 v0.1 束可被 v0.2 消费者在本文所述回退方式下消费。

### 13.1 破坏性变更

- **`timestamp` 被 `generated.at` 取代。** 概念的最后一次内容变更现在记录为 `generated: { by, at }`（§5.2）。当 `generated` 缺失时，消费者**可以（MAY）**回退到遗留的 `timestamp`。
- **正文 `# Citations` 列表被 `sources` 取代。** 溯源移入 frontmatter（§5.1）。消费者**应当（SHOULD）**读取 `sources`，并**可以（MAY）**仍为 v0.1 文档解析遗留的 `# Citations` 正文列表。

### 13.2 新增性变更

以下全部为新增：新的可选键、一种新概念类型、一个新约定标题。它们的缺失即产生一个普通的 v0.1 概念。

- 新 frontmatter 族：带逐来源可信度信号（`author`、`usage_count`、`last_modified`）及 `usage_window` 同级字段的 `sources`；`generated`、`verified`；`status`、`stale_after`（§5）。
- 新概念类型 `Attested Computation` 及其计算键 `runtime`、`parameters`、`computation`、`executor`、`attester`（§10）。
- 新约定正文标题 `# Computation`（§4.2）。
- 用于 `generated.by` 和 `verified[].by` 的行动者约定（§7）。

其余一切（束结构、保留文件名、必填的 `type`、推荐的 `title`/`description`/`resource`/`tags`、交叉链接、索引文件、日志文件、宽松合规性）均原样沿用。

---

## 附录 A：完整示例——损益表

一个覆盖每个族的束，展示为一份含两个数字（收入与毛利）的损益表从 v0.1 到 v0.2 的迁移。

### v0.1 形式

单份文档：两个数字在同一个概念中，SQL 以散文形式呈现（智能体可读、可忽略、可改写），引文为扁平列表，唯一时间戳是 `timestamp`。

```markdown
---
type: Metric
title: Income statement (fiscal year)
description: Headline income-statement figures for a fiscal year.
tags: [finance, income-statement]
timestamp: '2026-05-28T22:53:05+00:00'
---

# Definition
The income statement reports revenue and gross profit for a fiscal year.

# Revenue
Recognized revenue sums `amount` over rows booked to the fiscal year:

    SELECT SUM(amount) AS revenue
    FROM finance.recognized_revenue
    WHERE fiscal_year = <year>

# Gross profit
Gross profit by segment, per the cost-allocation standard:

    SELECT gross_profit FROM fct_income_statement
    WHERE fiscal_year = <year> AND segment = <segment>

# Citations
- https://wiki.acme/finance/fpa-handbook
- https://wiki.acme/finance/revenue-recognition
- https://wiki.acme/finance/cost-allocation
```

### v0.2 形式

两个数字拆分为由叙述性概念链接的证明计算。每个族都已填充，两个计算被刻意置于不同状态，使一个消费者得到两个裁决。

```
bundles/finance/
  metrics/income-statement.md      type: Metric  （叙述，链接二者）
  computations/revenue.md          type: Attested Computation  （runtime: bigquery）
  computations/profit.md           type: Attested Computation  （runtime: dbt）
  references/skills/run-on-bq.md, run-dbt.md
  references/attesters/sql-equality.py, dbt-binding.py
```

`metrics/income-statement.md`，可读文档；信任存在于它所链接之处，而不在此：

```markdown
---
type: Metric
title: Income statement (fiscal year)
description: Headline income-statement figures for a fiscal year.
tags: [finance, income-statement]
status: stable
generated: { by: reference_agent/gemini-2.5-pro, at: 2026-06-20T22:53:05Z }
verified: { by: human:ahormati, at: 2026-06-25T09:00:00Z }
stale_after: 2026-12-31
sources:
  - id: fpa-handbook
    resource: https://wiki.acme/finance/fpa-handbook
    title: FP&A reporting handbook
---

# Definition
The income statement reports [revenue](../computations/revenue.md) and
[gross profit](../computations/profit.md) for a fiscal year, per the FP&A
reporting handbook.[^fpa-handbook] Each figure is produced by a sanctioned,
attestable computation; this concept only narrates them.

[^fpa-handbook]: FP&A reporting handbook
```

`computations/revenue.md`，BigQuery SQL，人工验证，新鲜，并由一个携带可信度信号的活跃仪表盘来源佐证：

```markdown
---
type: Attested Computation
title: Revenue for fiscal year
description: Recognized revenue for a fiscal year, per Finance's definition.
tags: [finance, revenue]
status: stable
runtime: bigquery
parameters:
  - { name: year, type: integer, required: true }
executor:
  resource: references/skills/run-on-bq.md
  receipt: [job_id, executed_sql, result]
attester:
  resource: references/attesters/sql-equality.py
generated: { by: reference_agent/gemini-2.5-pro, at: 2026-06-28T14:00:00Z }
verified: { by: human:ahormati, at: 2026-06-25T09:00:00Z }
stale_after: 2026-12-31
sources:
  - id: rev-policy
    resource: https://wiki.acme/finance/revenue-recognition
    title: Revenue recognition policy
    author: team:finance-fpa
    last_modified: 2026-04-02
  - id: exec-rev-dash
    resource: dashboards/exec-revenue
    title: Executive revenue dashboard
    author: team:finance-fpa
    usage_count: 5000
    last_modified: 2026-06-18
usage_window: { from: 2026-06-01, to: 2026-06-30 }
---

# Computation

    SELECT SUM(amount) AS revenue
    FROM finance.recognized_revenue
    WHERE fiscal_year = @year

Recognized revenue per the recognition policy,[^rev-policy] corroborated by
the executive revenue dashboard.[^exec-rev-dash]

[^rev-policy]: Revenue recognition policy
[^exec-rev-dash]: Executive revenue dashboard
```

`computations/profit.md`，一个 dbt 模型，流程验证，且已过其 `stale_after`：

```markdown
---
type: Attested Computation
title: Gross profit for fiscal year
description: Gross profit by segment for a fiscal year, per the cost-allocation standard.
tags: [finance, profit]
status: stable
runtime: dbt
parameters:
  - { name: year, type: integer, required: true }
  - { name: segment, type: string, required: true }
executor:
  resource: references/skills/run-dbt.md
  receipt: [run_id, compiled_sql, result]
attester:
  resource: references/attesters/dbt-binding.py
generated: { by: reference_agent/gemini-2.5-pro, at: 2026-06-14T14:00:00Z }
verified: { by: process:finance-nightly, at: 2026-06-12T08:00:00Z }
stale_after: 2026-06-15
sources:
  - id: cost-alloc
    resource: https://wiki.acme/finance/cost-allocation
    title: Cost allocation standard
---

# Computation

    SELECT gross_profit
    FROM {{ ref('fct_income_statement') }}
    WHERE fiscal_year = {{ var('year') }}
      AND segment = {{ var('segment') }}

Gross profit by segment per the cost-allocation standard.[^cost-alloc]

[^cost-alloc]: Cost allocation standard
```
