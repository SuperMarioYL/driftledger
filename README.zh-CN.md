<div align="right"><sub><a href="./README.md">English</a>&nbsp;&nbsp;⇄&nbsp;&nbsp;<b>中文</b></sub></div>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/hero-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="./assets/hero-light.svg">
  <img src="./assets/hero-light.svg" width="880" alt="DriftLedger — 在第 20 分钟而非第 88 分钟发现智能体的计划漂移">
</picture>

<p align="center"><sub>面向长周期编程 / 研究智能体的计划-执行-偏差账本。</sub></p>

<p align="center">
  <a href="./LICENSE"><img alt="license" src="https://img.shields.io/badge/license-MIT-0071E3"></a>
  <a href="https://github.com/SuperMarioYL/driftledger/releases"><img alt="release" src="https://img.shields.io/github/v/release/SuperMarioYL/driftledger"></a>
  <a href="https://github.com/SuperMarioYL/driftledger/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/SuperMarioYL/driftledger/ci.yml?branch=main&label=CI"></a>
  <img alt="go" src="https://img.shields.io/badge/Go-1.24-0071E3?logo=go&logoColor=white">
  <img alt="Agentic" src="https://img.shields.io/badge/Agentic-plan%E2%86%94trace-5E5CE6">
</p>

**你的 Agent 在第 0 分钟给出了计划，第 20 分钟就悄悄偏航了——你却在第 88 分钟才发现。DriftLedger 在漂移发生的瞬间就把它标出来，贴在你的编码智能体旁边，并给你一个"接受"控制。**

DriftLedger 是 Addy Osmani 与 Boris Cherny 参与命名的 loop-engineering（循环工程）纪律的"在飞"续作——最接近 [cobusgreyling/loop-engineering](https://github.com/cobusgreyling/loop-engineering) 的实时对账版本：`loop-audit` 在第 88 分钟告诉你漂移，DriftLedger 在第 20 分钟就告诉你。你把智能体的计划发布为一份带版本号的契约，DriftLedger 在每一条新 trace 行上把它和实时执行做结构化对账，并在 bubbletea TUI 里渲染偏差账本，按 `a` 即可接受。免费、开源、单二进制、无需账号。

<details>
<summary>目录</summary>

- [架构](#架构)
- [为什么做这个](#为什么做这个)
- [安装](#安装)
- [快速开始](#快速开始)
- [用法](#用法)
- [演示](#演示)
- [配置](#配置)
- [路线图](#路线图)
- [许可证](#许可证)
</details>

<h2><img src="https://api.iconify.design/tabler:topology-star-3.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 架构</h2>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/atlas-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="./assets/atlas-light.svg">
  <img src="./assets/atlas-light.svg" width="880" alt="架构：plan.md + trace.jsonl → 对账器 → 偏差账本 + bubbletea TUI">
</picture>

一个 Go 二进制、一个进程，无守护进程、无网络。对账器在每一条新 trace 行上运行并重新输出偏差集合；账本是一个只追加的 JSONL 文件（`./driftledger.ledger.jsonl`），任何 `jq` 都能查。对账是**结构化**的——步骤存在性 + 验收关键词匹配——而不是 LLM 判分，因此确定、廉价，足以每条 trace 行都跑一次。

## 为什么做这个

长周期编程与研究智能体现在一跑就是几十分钟到几小时，而在这段时间里它们会悄悄偏离一开始的计划——重新解释范围、换工具、追分支。你只在事后才发现漂移，那时智能体已经烧掉了几小时、产出了偏靶的结果。DriftLedger 把已发布的计划与运行中智能体的实时执行绑定在一起，让你在飞行中接受漂移，把事后发现变成一条有版本的、一等公民的记录。

<h2><img src="https://api.iconify.design/tabler:rocket.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 安装</h2>

```bash
go install github.com/SuperMarioYL/driftledger@latest
```

或从源码构建：

```bash
git clone https://github.com/SuperMarioYL/driftledger
cd driftledger && go build -o driftledger ./cmd/driftledger
```

<h2><img src="https://api.iconify.design/tabler:rocket.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 快速开始</h2>

从冷克隆到看到第一条漂移，只需三条命令：

```bash
driftledger init -p plan.md                      # 生成一个 3 步、带版本的契约
# ...你的智能体在跑，一个 shim 把 trace.jsonl 追加上去...
driftledger watch plan.md trace.jsonl            # 实时偏差账本；按 `a` 接受
```

<details>
<summary><code>diff</code> 输出示例</summary>

```
$ driftledger diff plan.md trace.jsonl
plan 0.1.0  3 steps  3 trace events
------
step-1       matched     first-seen:2026-07-23T10:00:00Z
step-2       drifting    first-seen:2026-07-23T10:20:00Z  unmet: matched; drifting; unexecuted
step-3       unexecuted  first-seen:—
side-quest   extra       first-seen:2026-07-23T10:20:30Z  summary: rewrote the README hero instead
------
matched:1  drifting:1  unexecuted:1  extra:1
```
</details>

<h2><img src="https://api.iconify.design/tabler:terminal-2.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 用法</h2>

```bash
# 生成示例计划契约（带版本、3 步、验收关键词）。
driftledger init -p plan.md

# 把计划与 trace 的偏差打印到 stdout（非交互；会叠加账本里已接受的记录）。
# 适合放进 CI 或脚本化漂移检查。
driftledger diff plan.md trace.jsonl

# 在 TUI 里渲染实时偏差账本。会 tail trace.jsonl，每来一行就重新对账，
# 按 `a` 可在飞行中接受一条漂移。
driftledger watch plan.md trace.jsonl

# （m2/m3 路线图）patch 把契约改写为新版本；rollback 发出 git-revert 指令——
# 当前都是占位。
driftledger patch
driftledger rollback
```

**计划契约（`plan.md`）** —— markdown，每个步骤一个 `## <step-id>` 标题：

```markdown
# Plan: demo run

version: 0.1.0

## step-1
intent: Scaffold the project structure
accept: go module
accept: cmd package

## step-2
intent: Implement the structural reconciler
accept: matched
accept: drifting
accept: unexecuted
```

**Trace（`trace.jsonl`）** —— 每行一个 JSON 对象，由包裹智能体命令的薄 shim 实时追加：

```json
{"ts":"2026-07-23T10:00:00Z","step_id":"step-1","action":"run","summary":"initialized go module and added cmd package"}
```

每条验收准则，当其关键词（整词、≥3 字符、去掉停用词）全部出现在该步骤的 trace 摘要里时即视为满足。把准则写成步骤完成时必须出现的关键词名词 / 动词。

<h2><img src="https://api.iconify.design/tabler:photo.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 演示</h2>

![demo](assets/demo.gif)

10 分钟 happy path：`init` 一个 3 步计划，模拟智能体在第 20 分钟丢下 step-3 去改 README，`diff` 在漂移发生的瞬间就把它标出来。同样的对账会在 `watch` 里实时渲染，按 `a` 接受。完整 asciinema 录像在 `assets/demo.cast`。

<h2><img src="https://api.iconify.design/tabler:adjustments.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 配置</h2>

DriftLedger 无需配置文件——一切都是 CLI flag。两个输入文件加账本路径：

| 输入 | 路径 / flag | 格式 | 含义 |
|---|---|---|---|
| 计划契约 | `plan.md`（位置参数） | markdown | 智能体承诺要跑的带版本步骤；`version:` + `## <id>` + `intent:` + `accept:` 行。 |
| Trace 流 | `trace.jsonl`（位置参数） | JSONL | 每行一个事件：`ts`、可选 `step_id`、`action`、`summary`。 |
| 偏差账本 | `--ledger driftledger.ledger.jsonl` | JSONL | 只追加审计轨迹；`accept`/`patch`/`rollback` 条目，可用 `jq` 查。 |

<h2><img src="https://api.iconify.design/tabler:map-2.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 路线图</h2>

- [x] **m1 — 对账 + 实时 TUI**：`diff` 把偏差打印到 stdout；`watch` 在 bubbletea TUI 里渲染实时偏差账本，按 `a` 接受。这是一个周末能交付、值得星标的那一刀。
- [ ] **m2 — 改写契约**：`patch` 把 `plan.md` 改写为新的语义版本，捕获已接受的偏差；账本从此是带版本的审计轨迹。
- [ ] **m3 — 发出回滚指令**：`rollback` 为已接受的偏差发出 `git revert` + checkpoint-tag 指令（只发不执行）。随附用于 HN 发布的 asciinema 录像。
- [ ] Claude Code / deer-flow 转写格式的 trace shim；LLM 语义对账作为可选模式；漂移阈值告警。

### 与最接近的相邻工具对比

| 能力 | DriftLedger | [loop-engineering](https://github.com/cobusgreyling/loop-engineering) |
|---|---|---|
| 飞行中的实时对账 | ✓ | — |
| 计划即带版本契约 | ✓ | — |
| 事后循环审计（cost / init） | — | ✓ |
| 只追加、可 jq 查的账本 | ✓ | 部分 |
| 单二进制、零依赖 | ✓ | ✓ |
| 已有社区（8.9k★） | — | ✓ |

诚实说：loop-engineering 拿下了事后审计和受众；DriftLedger 只在你想要*智能体还在跑的时候*就把漂移标出来并接受时才有立足之地。

<h2><img src="https://api.iconify.design/tabler:license.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 许可证</h2>

MIT——见 [LICENSE](./LICENSE)。issue 与 PR 请提至 [github.com/SuperMarioYL/driftledger](https://github.com/SuperMarioYL/driftledger/issues)。

## 分享

```
DriftLedger — 面向长周期 Agent 运行的计划-执行-偏差账本。loop-audit 在第 88 分钟告诉你漂移；DriftLedger 在第 20 分钟就告诉你，并让你飞行中接受。https://github.com/SuperMarioYL/driftledger
```

<p align="center"><sub><a href="./LICENSE">MIT</a> © 2026 SuperMarioYL</sub></p>
