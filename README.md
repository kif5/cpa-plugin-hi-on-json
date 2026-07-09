# cpa-plugin-hi-on-json

CLIProxyAPI native plugin: 自动给“没有调用记录”的 auth JSON 发起一次 `Hi` 探测。

## 核心逻辑 v0.5.0

插件不再依赖长期状态文件，也不靠 `include_existing` 判断。

它直接读取 CLIProxyAPI 管理后台里每个 auth 的调用计数：

```text
成功 success + 失败 failed == 0  => 没调用过，触发一次 Hi
成功 success + 失败 failed > 0   => 已经有调用记录，不触发
```

因此无论 JSON 是手动上传、API 同步、复制进目录还是其它方式进来的，只要 CLIProxyAPI 能在 auth 列表里看到它，并且后台成功/失败计数都是 0，插件就会补一次 `Hi`。

## 推荐配置

```yaml
plugins:
  enabled: true
  dir: "/opt/CLIProxyAPI/plugins"
  configs:
    hi-on-json:
      enabled: true
      priority: 1
      model: "gpt-5.5"
      prompt: "Hi"
      poll_interval: "2s"
      settle_delay: "3s"
      retry_failed: true
      retry_interval: "30s"
      trigger_cooldown: "10m"
      persist_state: false
      name_suffix: ".json"
```

说明：

- `persist_state: false`：v0.5.0 推荐关闭，不需要状态文件。
- `trigger_cooldown: "10m"`：插件自己发完 Hi 后，等待后台成功计数更新期间，避免同一个 auth 被连续触发多次。
- `retry_failed: true`：如果 Hi 请求失败，后面继续重试。

## 构建

需要 Go + C compiler（Windows 需要 MinGW-w64/GCC）。

Windows：

```powershell
go mod tidy
go build -buildmode=c-shared -ldflags "-s -w" -o hi-on-json.dll .
```

Linux/macOS：

```bash
go mod tidy
go build -buildmode=c-shared -ldflags "-s -w" -o hi-on-json.so .
```

把生成的 `hi-on-json.dll` / `hi-on-json.so` 放进 CLIProxyAPI 配置的 `plugins.dir`。
