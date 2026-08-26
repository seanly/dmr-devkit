# Script 工具协议

`tools/script` 把磁盘上的**自描述可执行文件**桥接成 `*tool.Tool`，无需重新编译 Go、也无需起 MCP 服务器。协议借鉴 [shell-operator](https://github.com/flant/shell-operator) 的 `--config` 模式：脚本被问时描述自己，不被问时干活。

本文件是协议的**权威规范**。dmr 的 `script` 插件、okf-devkit、rca-platform 等宿主共用此引擎。

> 与 MCP 的区别：MCP 适合「外部服务、多工具、有状态会话」；script 工具适合「快速一次性小工具」（查库、跑变换、抓接口）。

默认每个脚本工具归入 `extended` 组，经 `toolSearch` 按需发现，不会撑大系统提示。`devkit.Build` **不会**自动注册它们——宿主调用 `script.Load` 后自行 `append` 到 `Options.Tools`。

## 两种调用模式

```
加载时:  <script> --config     → stdout 打印 JSON Spec（5s 超时）
运行时:  <script>              → 经 TOOL_* 临时文件交换入参/结果
```

任意语言通过 shebang 即可（`#!/usr/bin/env bash`、`#!/usr/bin/env python3` …）。

## 配置模式 — `<script> --config`

启动时每个被发现的脚本被探测一次。stdout 必须是一份 JSON `Spec`，退出码 0。探测超时 **5 秒**。

```json
{
  "name": "my_tool",
  "description": "工具做什么，给 LLM 看。",
  "parameters": {
    "type": "object",
    "properties": { "x": { "type": "string", "description": "..." } },
    "required": ["x"]
  },
  "group": "extended",
  "timeout": "30s",
  "search_hint": "keyword, keywords, for, toolsearch"
}
```

| 字段 | 引擎要求 | 说明 |
|------|----------|------|
| `name` | **必填** | 逻辑名。加载时加前缀（默认 `script_`），如 `echo` → `script_echo`，与内置工具、MCP 工具（`mcp_` 前缀）隔离。 |
| `parameters` | **必填**（非 `null`） | 参数的 JSON Schema。缺 `name` 或 `parameters` 的脚本会被跳过。 |
| `description` | 建议 | 给 LLM 看的描述。引擎不强制，但缺了 LLM 几乎不会调用。 |
| `group` | 可选 | `core` / `extended` / `mcp`。缺省或非法值回落 `extended`。 |
| `timeout` | 可选 | Go duration（`30s`、`2m`）。默认 `60s`。非法值告警后回落默认。超时会杀进程（见下方）。 |
| `search_hint` | 可选 | `toolSearch` 匹配的关键词。 |

前缀可通过 `Options.NamePrefix` 覆盖；空字符串仍使用默认 `script_`。

## 运行模式 — `<script>`（无 `--config`）

执行实际工作。I/O 通过**环境变量指向的临时文件**，stdout/stderr 只作日志，不作为结果解析。

引擎先创建临时目录，写入 `args.json`，并预创建空的 `result.json`（脚本可以假定该文件已存在）。工作目录 `cmd.Dir` 是**脚本所在目录**——脚本里的相对路径相对脚本目录，不是 agent workspace。要用工作区请读 `TOOL_WORKSPACE`。

| 环境变量 | 方向 | 内容 |
|----------|------|------|
| `TOOL_ARGS_PATH` | 输入 | JSON 文件，含经 schema 校验的参数 map |
| `TOOL_RESULT_PATH` | 输出 | 把结果 JSON 写到这里 |
| `TOOL_WORKSPACE` | 上下文 | agent 工作区目录（非空才注入） |
| `TOOL_TAPE` | 上下文 | 当前 tape 名（非空才注入） |
| `TOOL_RUN_ID` | 上下文 | 当前 run id（非空才注入） |
| `Options.StaticEnv` | 静态 | 宿主注入的常量，如 `OKF_BUNDLE_ROOT` |

- **退出 0** = 成功。读取 `TOOL_RESULT_PATH` 并 JSON 解码为工具结果；空文件视为 `null`。非法 JSON 返回错误。
- **非 0 退出** = 失败。stdout+stderr 合并后截取末 512 字节，折进返回给 agent 的错误信息。
- **超时**：默认 60s（或 Spec 声明的 `timeout`）。Unix 上脚本在独立进程组里运行，超时对整个组发 `SIGKILL`（子进程如 `sleep`/`curl` 一并杀掉）。Windows 只杀主进程。
- **stdout/stderr** 进入运行日志，不作为结果。用它们输出人类可读进度。

## 发现规则

`Load` 递归扫描给定目录，仅当文件满足以下条件才作为工具加载：

- **可执行**（`chmod +x`，权限位含任一 `+x`）；
- 名字不以 `.` 开头（隐藏文件/目录整棵跳过）；
- 不在**顶层** `lib/` 目录下（共享库存放处；嵌套的 `foo/lib/` 不会被跳过）；
- 不是数据扩展名（`.md` `.yaml` `.yml` `.json` `.txt`）。

文件按路径排序，故数字前缀（`001-`、`002-`）给出确定加载顺序。

坏脚本（`--config` 失败、JSON 非法、缺 `name`/`parameters`、逻辑名与已加载脚本重复）会被跳过并 `slog.Warn`，**永不阻断** `Load` 返回。目录本身无法遍历时才返回 error。

## Go API

```go
import "github.com/seanly/dmr-devkit/tools/script"

tools, err := script.Load(ctx, "/path/to/tools.d", script.Options{
    // NamePrefix: "script_",           // 默认；空字符串同样回落此值
    StaticEnv: map[string]string{
        "OKF_BUNDLE_ROOT": bundleRoot, // 每次运行都注入
    },
})
opts.Tools = append(opts.Tools, tools...)
```

`Options`：

| 字段 | 默认 | 说明 |
|------|------|------|
| `NamePrefix` | `"script_"` | 加在 Spec `name` 前面。空则用默认。 |
| `StaticEnv` | 无 | 追加到每次运行的环境变量，适合不随 turn 变化的宿主上下文。 |

脚本路径在加载时解析为绝对路径，因此 `tools` 目录可以是相对路径。

## 模板

### Bash

```bash
#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "--config" ]]; then
  cat <<'EOF'
{"name":"my_tool","description":"...","parameters":{"type":"object","properties":{"x":{"type":"string"}},"required":["x"]},"timeout":"10s"}
EOF
  exit 0
fi

args=$(cat "$TOOL_ARGS_PATH")
x=$(printf '%s' "$args" | jq -r '.x // empty')
printf '{"x":%s}' "$(jq -Rn --arg v "$x" '$v')" > "$TOOL_RESULT_PATH"
```

共享辅助函数可放在顶层 `lib/`（发现规则会跳过该目录），例如 `source "$(dirname "$0")/lib/tool_lib.sh"`。

### Python

```python
#!/usr/bin/env python3
import json, os, sys

if len(sys.argv) > 1 and sys.argv[1] == "--config":
    print(json.dumps({
        "name": "my_tool",
        "description": "...",
        "parameters": {"type": "object", "properties": {"x": {"type": "string"}}, "required": ["x"]},
        "timeout": "10s",
    }))
    sys.exit(0)

args = json.load(open(os.environ["TOOL_ARGS_PATH"]))
json.dump({"x": args["x"]}, open(os.environ["TOOL_RESULT_PATH"], "w"))
```
