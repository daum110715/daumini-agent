# daumini-agent

完全独立运行的纯本地 Agent 工具（Go 核心引擎 + 本地 Web 工作台 + 自适应浅色/深色终端 CLI），开箱不依赖远程控制端，可直连 DeepSeek、OpenAI、Anthropic、任意 OpenAI 兼容接口或 `daumini` 中转站。

## 特性

- **纯本地独立运行**：`daumini-agent web` 直接在 `127.0.0.1:3847` 起本地界面并自动打开浏览器；`daumini-agent` 直接进入终端交互会话
- **浅色/深色终端原生适配**：自动通过 `OSC 11`、`COLORFGBG`、`NO_COLOR` 检测终端背景亮度，正文保持终端默认前景色，在浅色白底/米色终端下同样高对比清晰可读（支持 `--theme light|dark|auto` 与 `/theme` 实时切换）
- **Harness 工程实战约束**：
  - `fs_read` 返回 `line#hash` 行哈希锚点，`fs_edit` 强制先读后改并校验行哈希
  - 工作区路径 + Symlink 逃逸拦截，非交互 `shell_exec` 仅以 `exit_code != 0` 判错，超时按进程组整棵清理，大输出落盘 `.agent-logs/`
  - 静态前缀零动态字段，动态状态通过尾部 `<system-reminder>` 注入；批量观察输出遮罩（Observation Masking）防上下文退化与缓存抖动
  - Append-only JSONL 事件日志（`events.jsonl`）、断线 `seq` 重放、会话分叉（`/fork`）、重启孤儿 `tool_use` 自动修补、Doom Loop 检测
- **扩展能力**：
  - **MCP 客户端**：支持 `stdio` 子进程与 `Streamable HTTP` 传输，自动注册 `mcp__<server>__<tool>`
  - **Skills（`SKILL.md`）**：自动扫描工作区 `.daumini-agent/skills/`、`.agents/skills/` 与用户目录 `~/.config/daumini-agent/skills/`
  - **Hooks 与插件工具**：支持 `pre_run` / `post_run` / `pre_tool` / `post_tool` / `stop_check` 外部命令钩子及可执行插件工具
  - **Leaf Subagent**：有界深度 1 的隔离只读/局部子代理调研汇总

## 快速开始

```sh
# 交互式 CLI（默认自动适应浅色或深色终端）
daumini-agent --dir /path/to/workspace

# 启动本地 Web 工作台并打开浏览器
daumini-agent web --dir /path/to/workspace

# 无头单次执行
daumini-agent run --dir . --yolo "检查测试失败原因"
```

## 常用配置

配置文件位于 `~/.config/daumini-agent/config.json`（权限 `0600`）：

```sh
# 直接写入 Provider Key（DeepSeek / OpenAI / Anthropic / 自定义端点）
daumini-agent config set-key deepseek sk-xxxx deepseek-chat

# 或一键登录 daumini 中转账号签发本地密钥
daumini-agent config login https://mini.daum.pw
```

## 构建

```sh
# 编译本地 Web 资产后打包 Go 二进制
npm --prefix web run build
cp -r web/dist/* internal/webui/dist/
go build -o daumini-agent ./cmd/daumini-agent
```
