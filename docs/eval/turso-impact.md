# Turso 影响面评估报告

**日期:** 2026-06-24  
**tursogo 版本:** `turso.tech/database/tursogo v0.6.1`  
**评估范围:** dmr-devkit tape store + dmr plugins (memory/kanban/handlog/llmwiki) SQLite 用法

## 决策：**No-Go**（暂不集成 Turso 为 production driver）

DMR 默认依赖 **FTS5 trigram** 全文检索（tape `tapeSearch`、memory、handlog、llmwiki）。当前 **tursogo 不支持 SQLite FTS5 模块**（`no such module: fts5`），与核心能力不兼容。在 Turso 侧提供 FTS5 或 DMR 可接受的全文检索替代方案之前，**不建议**将 `modernc.org/sqlite` 替换为 Turso。

**Partial 观察：** 无 FTS 的基础 CRUD（tape T01–T03、T12–T13、T15；插件 M01/M03/K01）在 tursogo 上 **PASS**，仅适合作为未来「LIKE-only / 无 FTS」降级路径的参考，不满足现有产品默认配置。

**Turso 原生 FTS（Tantivy）补充评估：** Turso **有**全文检索，但 **不是 SQLite FTS5**（见 [官方 FTS 文档](https://docs.turso.tech/sql-reference/functions/fts)）。已增加 F01–F08 / P01–P03 用例（`CREATE INDEX ... USING fts` + `fts_match` + `tokenizer=ngram`）。在 **tursogo v0.6.1 预编译 Go 运行时** 上，F00 探测 **SKIP**：`unknown module name 'fts'`（与 [tursodatabase/turso#6255](https://github.com/tursodatabase/turso/issues/6255) 一致）。因此 **「换用 Turso 全文检索」在 Go 嵌入路径上当前也无法验证/落地**；即便将来可用，也需重写 tape/插件检索层（无 FTS5 虚拟表/trigger，改 `fts_match`/`fts_score`，并处理 `OPTIMIZE INDEX`）。

---

## 1. Tape 兼容性矩阵（T01–T15）

| 用例 | modernc | tursogo | 差异说明 | 阻塞级别 |
|------|---------|---------|---------|----------|
| T01 Schema | PASS | PASS | | ok |
| T02 CRUD | PASS | PASS | | ok |
| T03 json_extract anchor | PASS | PASS | | ok |
| T04 FTS5 trigram 建表 | PASS | **FAIL** | `no such module: fts5` | **blocker** |
| T05 FTS INSERT trigger | PASS | **FAIL** | 同上 | **blocker** |
| T06 FTS DELETE trigger | SKIP | **FAIL** | modernc：DELETE 触发 SQL logic error；turso：无法建 FTS | warn |
| T07 中英文 FTS 搜索 | PASS | **FAIL** | | **blocker** |
| T08 特殊字符 FTS | PASS | **FAIL** | | warn |
| T09 多 tape FTS 隔离 | PASS | **FAIL** | | **blocker** |
| T10 FTS 迁移 backfill | PASS | **FAIL** | | **blocker** |
| T11 RebuildFTS | PASS | **FAIL** | | warn |
| T12 LIKE fallback | PASS | PASS | | ok |
| T13 并发 Append | PASS | PASS | 10×20 goroutine，无丢行 | ok |
| T14 WAL | PASS | SKIP | turso 不适用 WAL pragma 路径 | ok |
| T15 Timezone | PASS | PASS | | ok |

**汇总：** tursogo **5 个 blocker**（T04/T05/T07/T09/T10），**8 个 FAIL** 总计。

评估代码：[`tape/sqlcompat/`](../tape/sqlcompat/)（SQLite FTS5：`TestEvalTursoCompare`；Turso 原生 FTS：`TestEvalTursoNativeFTS`）

### 1.1 Turso 原生 FTS 矩阵（F01–F08，tursogo + `?experimental=index_method`）

| 用例 | 对应 DMR 场景 | tursogo v0.6.1 | 说明 |
|------|---------------|---------------|------|
| F00 | `CREATE INDEX ... USING fts` 探测 | **SKIP** | `unknown module name 'fts'` — 预编译 runtime 未编入 FTS |
| F01 | tape entries + ngram 索引 | （未跑） | F00 失败则整组 Skip |
| F02 | 中文 trigram → `tokenizer=ngram` | （未跑） | |
| F03 | 英文搜索 | （未跑） | |
| F04 | 多 tape + `WHERE tape=?` | （未跑） | |
| F05 | IP/URL 特殊字符 | （未跑） | |
| F06 | INSERT 自动建索引 | （未跑） | 无 trigger，依赖 Turso DML 更新 |
| F07 | DELETE + OPTIMIZE | （未跑） | |
| F08 | 批量写入后 OPTIMIZE | （未跑） | |

插件原生 FTS（`TestPluginsTursoNativeFTS`）：P01 memory / P02 handlog / P03 llmwiki — 同样 **F00/P00 SKIP**。

**若 Go 运行时编入 FTS 后仍 No-Go 的因素：** API 与实现与 SQLite FTS5 不兼容，tape `fetchWithFTS5`、memory/handlog 虚拟表/trigger 需分支重写；迁移需 `OPTIMIZE INDEX` 而非 `RebuildFTS()`。

---

## 2. 插件冒烟矩阵（M/K/H/W）

| 用例 | modernc | tursogo | 差异说明 | 阻塞级别 |
|------|---------|---------|---------|----------|
| M01 memory schema | PASS | PASS | | ok |
| M02 memory FTS5 | PASS | **FAIL** | `no such module: fts5` | **blocker** |
| M03 memory foreign_keys | PASS | PASS | CASCADE 正常 | ok |
| K01 kanban schema+CRUD | PASS | PASS | 无 FTS | ok |
| H01 handlog FTS schema | PASS | **FAIL** | content=records FTS5 | **blocker** |
| W01 llmwiki FTS schema | PASS | **FAIL** | concepts_fts | **blocker** |

**汇总：** **3 个 blocker**（M02/H01/W01）。

评估代码：[`dmr/eval/turso/`](../../dmr/eval/turso/)（dmr 仓库）

---

## 3. Turso Sync（S01–S05）

| 状态 | 说明 |
|------|------|
| **未验证** | 无 `TURSO_SYNC_URL` / `TURSO_AUTH_TOKEN` 环境；spike 测试已实现并默认 `Skip` |

Spike 入口：`go test -tags turso_sync_eval ./tape/sqlcompat/ -run TestSyncSpike`

**Sync ROI：** 在 FTS blocker 未解决前 **Defer**；即使 Sync 可用，也无法同步 tape FTS 索引语义。

---

## 4. 非功能评估（N01–N05）

| ID | 结果 | 记录 |
|----|------|------|
| N01 Binary size | sqlcompat 测试二进制 ~20MB → ~33MB（+~12MB，含 tursogo 嵌入 native lib） | 显著增大，嵌入 agent 需权衡 |
| N02 依赖 | 新增 `turso.tech/database/tursogo`、`turso-go-platform-libs`、`purego` | 仅 eval 路径引入 devkit go.mod |
| N03 平台 | 本机 dar/darwin 通过 | linux 需在 CI 矩阵验证 |
| N04 BETA | [Turso Go SDK](https://docs.turso.tech/sdk/go/quickstart) 标注 BETA | 生产数据需备份策略 |
| N05 vs PG | 多实例 tape 已有 **PostgreSQL + tsvector** | Turso Cloud 远程路径 ROI **低** |

---

## 5. 代码改动影响面（若强行 Go 的预估）

| 区域 | 文件 | 估行数 | 备注 |
|------|------|--------|------|
| devkit tape | `factory.go`, `sqlite_store.go` | 80–150 | 已加 `NewSQLiteTapeStoreWithDriver` + `DB()` 供评估 |
| dmr config | `config.example.toml` | ~20 | **No-Go 下不做** |
| memory/kanban/handlog/llmwiki | 各 `store_sqlite.go` | 20×4 | FTS 插件全部受阻 |
| CI | build tag `turso_eval` | ~15 | 已具备 |

---

## 6. 能力 ROI（在兼容前提下）

| Turso 能力 | 解决痛点 | 现有替代 | ROI |
|-----------|---------|---------|-----|
| MVCC 并发写 | 多 subagent 写 tape | 进程 RWMutex；T13 两者均 PASS | **低**（未观察到 modernc 丢数据） |
| Turso Sync | cloud ↔ local tape | localbridge 仅路由工具 | **未验证** |
| Vector search | 语义检索 | FTS5 / PG tsvector | **Defer**（需 embedding 管道） |
| Turso Cloud 远程 | 多实例 | **PG tape** | **低** |
| AgentFS | fs 沙箱 | tape + OPA；**无 Go SDK** | **排除** |

---

## 7. Go/No-Go 对照

| 决策 | 条件 | 本次 |
|------|------|------|
| Go — tape turso driver | T01–T12 PASS，blocker=0 | **不满足** |
| Go — turso-sync | S02/S03 PASS + 业务需求 | **未验证** |
| Partial — 仅 LIKE tape | Tape 无 FTS 路径 | 理论可行，**不符合默认 enable_fts5=true** |
| **No-Go** | FTS5/json_extract blocker | **✓ 采用** |
| Defer | 6 个月复评 | Turso Go 预编译编入 **native FTS** 后重跑 `TestEvalTursoNativeFTS` |

---

## 8. 后续行动

1. **维持** `modernc.org/sqlite` 为默认 driver。
2. **保留** `tape/sqlcompat` + `eval/turso` harness，Turso 发版后重跑：  
   `go test -tags turso_eval ./tape/sqlcompat/ -run TestEvalTursoCompare -v`  
   `go test -tags turso_eval ./tape/sqlcompat/ -run TestEvalTursoNativeFTS -v`
3. **不实施** Phase 1 `driver=turso` / Phase 2 Turso Sync（除非产品明确接受 FTS 降级且文档化）。
4. 多实例场景继续推荐 **[tape] driver=postgres**。
5. 跟踪 [tursodatabase/turso#6255](https://github.com/tursodatabase/turso/issues/6255) — Go 预编译 `tursogo` 编入 native FTS 后再评；**不要假设**会支持 SQLite FTS5。

---

## 9. 如何复现

见 [`tape/sqlcompat/README.md`](../tape/sqlcompat/README.md)。
