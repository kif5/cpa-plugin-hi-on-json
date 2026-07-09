# cpa-plugin-hi-on-json

CLIProxyAPI native plugin: 新 auth JSON 被 CLIProxyAPI 识别后，自动走宿主 `host.model.execute` 问一句 `Hi`。

## 构建

需要 Go + C compiler（Windows 需要 MinGW-w64/GCC）。

```powershell
cd C:\Users\Administrator\Downloads\V3.5\fx\cpa-plugin-hi-on-json
go mod tidy
go build -buildmode=c-shared -ldflags "-s -w" -o hi-on-json.dll .
```

Linux/macOS：

```bash
go mod tidy
go build -buildmode=c-shared -ldflags "-s -w" -o hi-on-json.so .
```

把生成的 `hi-on-json.dll` / `hi-on-json.so` 放进 CLIProxyAPI 配置的 `plugins.dir`。

## CLIProxyAPI 配置示例

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    hi-on-json:
      enabled: true
      priority: 1
      model: "gpt-5.5"
      prompt: "Hi"
      poll_interval: "2s"
      settle_delay: "3s"
      include_existing: true
      persist_state: true
      trigger_on_update: true
      retry_failed: true
      retry_interval: "30s"
      trigger_cooldown: "10m"
      # providers: ["openai", "codex"]   # 可选：只监听这些 provider
      name_suffix: ".json"
```

## 防漏与防重复建议

v0.4.0 起推荐线上这样配：

```yaml
include_existing: true     # 避免 API 同步期间/重启期间新增的 JSON 被 baseline 跳过
persist_state: true        # 持久化已处理列表，避免重启后旧 JSON 全部重触发
trigger_on_update: true    # 同名/同账号 JSON 文件更新时间或大小变化时也触发
retry_failed: true         # Hi 请求失败时不标记完成，后面继续重试
retry_interval: "30s"      # 失败后重试间隔
trigger_cooldown: "10m"    # 同一个 auth 因 WRITE 更新再次触发的最小间隔；0s 表示关闭冷却
```

如果同步工具会批量重写所有 JSON，建议 `trigger_cooldown` 设成 `10m` 到 `60m`，避免一轮同步造成重复调用。

状态页会显示 `state_path`，默认状态文件在插件目录下：`hi-on-json.state.json`。
