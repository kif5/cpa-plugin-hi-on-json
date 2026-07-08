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
      include_existing: false
      # providers: ["openai", "codex"]   # 可选：只监听这些 provider
      name_suffix: ".json"
```

`include_existing: false` 表示插件启动时已有的 JSON 不触发，只对之后新落地/新识别的 JSON 触发。
