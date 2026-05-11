# 工作流接入告警场景

将 `workflow` 包与监控系统（Prometheus、Grafana、PagerDuty 等）结合，实现告警自动分级响应。

## 接入模式对比

| 模式 | 延迟 | 复杂度 | 适用场景 |
|------|------|--------|----------|
| Webhook 推送 | 实时 | 低 | 支持 webhook 的监控系统（推荐） |
| 轮询拉取 | 分钟级 | 低 | 无 webhook 或内网部署 |
| 消息队列 | 实时 | 中 | 大规模、多渠道告警 |

## Webhook 推送（推荐）

用 `net/http` 启动一个独立的 webhook 服务器，收到告警后触发 Graph 工作流。

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "time"

    "github.com/seanly/dmr-devkit/devkit"
    "github.com/seanly/dmr-devkit/workflow"
)

// AlertPayload 是 Prometheus Alertmanager 的典型 webhook 格式
type AlertPayload struct {
    Status string `json:"status"`
    Alerts []struct {
        Labels      map[string]string `json:"labels"`
        Annotations map[string]string `json:"annotations"`
        Status      string            `json:"status"`
    } `json:"alerts"`
}

func main() {
    ctx := context.Background()

    // 1. Build devkit agent
    opts := devkit.EnvOptions()
    if opts.APIKey == "" || opts.Model == "" {
        log.Fatal("AI_API_KEY and AI_MODEL are required")
    }
    opts.Verbose = 1

    kit, err := devkit.Build(ctx, opts)
    if err != nil {
        log.Fatal(err)
    }
    defer func() { _ = kit.Close(ctx) }()

    // 2. 构建告警处理工作流
    handleAlert := func(payload AlertPayload) {
        // 取第一个 firing 的告警
        var alertMsg string
        for _, a := range payload.Alerts {
            if a.Status == "firing" {
                alertMsg = a.Annotations["summary"]
                if alertMsg == "" {
                    alertMsg = a.Annotations["description"]
                }
                break
            }
        }
        if alertMsg == "" {
            log.Println("no firing alert found")
            return
        }

        g := buildIncidentGraph(kit)

        // 每个告警用独立 tape，便于审计
        tapeName := fmt.Sprintf("alert-%d", time.Now().Unix())
        res, err := kit.RunWorkflow(ctx, g, alertMsg)
        if err != nil {
            log.Printf("workflow failed: %v", err)
            return
        }
        log.Printf("alert handled: tape=%s steps=%d", tapeName, res.Steps)
    }

    // 3. 启动 webhook 服务器
    http.HandleFunc("/webhook/alerts", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
            return
        }
        var payload AlertPayload
        if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
            http.Error(w, "invalid payload", http.StatusBadRequest)
            return
        }
        go handleAlert(payload) // 异步处理，不阻塞 HTTP 响应
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"status":"ok"}`))
    })

    addr := ":8080"
    log.Printf("webhook server listening on %s", addr)
    log.Fatal(http.ListenAndServe(addr, nil))
}

// buildIncidentGraph 构建告警分级响应图
func buildIncidentGraph(kit *devkit.Kit) *workflow.Graph {
    g := &workflow.Graph{Name: "incident_response"}

    // classify 节点：AI 判断严重级别
    classify := kit.AsAgentNodeWithTape("classify", "alert-classify")
    classify.SystemPrompt = `You are an SRE classifier. Analyze the alert and reply with exactly one word:
- CRITICAL: service down, data loss, or revenue impact
- WARNING: degraded performance or elevated errors
- INFO: routine metrics or non-actionable events
Only output one word, no explanation.`
    g.AddNode("classify", classify)

    // 三个分支处理节点
    critical := kit.AsAgentNodeWithTape("critical", "alert-critical")
    critical.SystemPrompt = "You are an on-call SRE. The incident is CRITICAL. Propose the fastest mitigation. Be terse."
    g.AddNode("critical_handler", critical)

    warning := kit.AsAgentNodeWithTape("warning", "alert-warning")
    warning.SystemPrompt = "You are an SRE. The incident is WARNING. Provide a 3-step troubleshooting checklist. Be terse."
    g.AddNode("warning_handler", warning)

    info := kit.AsAgentNodeWithTape("info", "alert-info")
    info.SystemPrompt = "You are an SRE. The incident is INFO. Write a one-line summary. Be terse."
    g.AddNode("info_handler", info)

    // 边与路由
    g.AddEdge("START", "classify")
    g.AddConditionalEdges("classify",
        workflow.Default(
            workflow.ContainsRouter(map[string]string{
                "critical": "CRITICAL",
                "warning":  "WARNING",
            }),
            "info",
        ),
        map[string]string{
            "critical": "critical_handler",
            "warning":  "warning_handler",
            "info":     "info_handler",
        },
    )

    return g
}
```

**Prometheus Alertmanager 配置示例：**

```yaml
receivers:
  - name: "dmr-webhook"
    webhook_configs:
      - url: "http://dmr-webhook:8080/webhook/alerts"
        send_resolved: false
```

## 轮询拉取

如果监控系统不支持 webhook（或部署在内网无法被推送），可以定时轮询拉取：

```go
// 定时检查监控 API
func pollAlerts(ctx context.Context, kit *devkit.Kit) {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            alerts, err := fetchAlertsFromAPI() // 调用监控提供商 API
            if err != nil {
                log.Printf("fetch alerts error: %v", err)
                continue
            }
            for _, alert := range alerts {
                g := buildIncidentGraph(kit)
                _, _ = kit.RunWorkflow(ctx, g, alert.Message)
            }
        }
    }
}
```

## 消息队列（大规模场景）

如果有 Kafka / RabbitMQ / Redis Stream，可以用消费者触发工作流：

```go
alertConsumer := workflow.NodeFunc{
    N: "alert_consumer",
    F: func(ctx context.Context, wctx *workflow.Context, _ any) (any, error) {
        msg, err := kafkaReader.ReadMessage(ctx)
        if err != nil {
            return nil, err
        }
        return string(msg.Value), nil
    },
}

// 每条消息触发一次工作流
g := &workflow.Graph{Name: "mq_incident"}
g.AddNode("consumer", alertConsumer)
g.AddNode("classify", classifier)
g.AddEdge("START", "consumer")
g.AddEdge("consumer", "classify")
// ... 后续分支
```

## 关键设计要点

1. **异步处理** — Webhook handler 应该 `go handleAlert(payload)` 异步执行，避免告警发送方超时
2. **独立 Tape** — 每个告警用独立的 `TapeName`（如 `alert-<timestamp>`），便于后续审计和追溯
3. **工作流复用** — `buildIncidentGraph` 可以对不同类型的告警返回不同的 Graph，也可以用同一个 Graph 通过 `SystemPrompt` 区分场景
4. **过滤已处理告警** — 建议在 handleAlert 中维护一个已处理告警 ID 的集合，避免重复处理 firing 状态的告警

## 运行

```bash
AI_API_KEY=... AI_MODEL=gpt-4o-mini go run main.go
```

然后在 Alertmanager 中配置 webhook 指向 `http://<host>:8080/webhook/alerts`。
