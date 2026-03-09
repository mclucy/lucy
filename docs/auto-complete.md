# Auto Complete 框架回顾与扩展参考

## 1. 文档目的

本文用于总结 `feat/auto-complete` 的更改，明确当前可用的自动补全框架边界，并给出后续扩展到更多候选、高级策略、以及联网补全的可执行路线。

核心目标：

- 回退过度实现（动态聚合/探测型候选）。
- 保留稳定框架（命令级 `ShellComplete` + 运行时脚本生成）。
- 为下一阶段扩展保留干净、可插拔、可验证的基础。

## 2. 当前 auto complete 框架分层

### 2.1. 入口层（CLI 框架）

- `cmd/cmd.go` 保持 `EnableShellCompletion: true`。
- 运行时生成脚本仍可用：
  - `lucy completion bash`
  - `lucy completion zsh`
  - `lucy completion fish`
  - `lucy completion pwsh`

### 2.2. 命令层（命令自定义 ShellComplete）

- `cmd/cmd_add.go`
  - 仅关心位置参数平台段。
  - `ParseCompletionToken` + `FilterByPrefix` + 静态平台候选。

- `cmd/cmd_info.go`
  - 通过 `os.Args` 判断是否在补全 `--source` 的值。
  - source 场景与位置参数场景分流。

- `cmd/cmd_search.go`
  - 通过向后扫描 `os.Args`（跳过 `--generate-shell-completion`）定位当前 flag 上下文。
  - `--index` / `--source` / 位置参数三路分流。

### 2.3. 公共能力层

- `cmd/cmd_completion_helpers.go` 提供：
  - `CompletionCandidate`
  - `PrintCandidates`（`value:description` 输出契约）
  - `FilterByPrefix`（大小写不敏感前缀匹配）
  - `StaticPlatformCandidates`
  - `StaticSourceCandidates`
  - `StaticSortCandidates`
  - `ParseCompletionToken`

关键安全点：

- 补全路径不调用 `syntax.Parse()`，避免 partial token 导致 panic。

## 3. 当前已验证基线

以下能力在本轮收尾验证中通过：

- `go build -o /tmp/lucy-ac .` 成功。
- `go test ./...` 成功。
- `cmd/` 下无动态聚合残留符号：
  - `AggregatePackageCandidates`
  - `CacheDerivedCandidates`
  - `LocalInstalledCandidates`
  - `DeduplicateCandidates`
- 补全冒烟矩阵通过：
  - `add` 平台补全与前缀补全
  - `info --source` 与 `info` 位置参数补全
  - `search --index` / `search --source` / 位置参数补全
  - `completion bash|zsh|fish|pwsh` 脚本非空

## 4. 后续扩展必须遵守的规则

1. 不改变命令语义：补全只影响候选提示，不影响 Action 行为。
2. 不允许 panic：任何 partial/非法 token 都要安全退化。
3. 可降级：候选源失败时返回空或回退，不中断补全。
4. 输出契约稳定：持续使用 `value:description`。
5. 四 shell 兼容：bash/zsh/fish/pwsh 全部可用。
6. `os.Args` 路由保留：flag-value 场景必须可靠识别。

## 5. 建议扩展路线图

### 5.1. 阶段 A：静态能力增强（不联网）

目标：在不引入动态源的情况下提高可用性。

- 丰富静态候选描述（更清晰的说明与别名）。
- 引入稳定排序：精确前缀 > 普通前缀 > 其他。
- 统一 `info` 与 `search` 的 flag 上下文识别写法。
- 为 helper 补齐表驱动单测：`ParseCompletionToken`、`FilterByPrefix`。

### 5.2. 阶段 B：本地动态候选（离线）

目标：在零网络依赖下提供更多实用候选。

- 引入本地 Provider（可插拔）：
  - 本地安装元数据
  - 本地缓存索引
  - 最近使用历史
- 每个 Provider 采用 fail-open 策略（失败不影响主流程）。
- 设置补全链路耗时预算（例如 50~100ms）。

推荐候选链路：

1. 静态候选
2. 本地索引候选
3. 本地历史候选
4. 合并去重 + 排序 + 截断输出

### 5.3. 阶段 C：联网补全（可选、可回退）

目标：在不牺牲响应速度和稳定性的前提下，加入在线候选。

- 在线 Provider 默认可配置（开/关）。
- 必须配套：超时、限流、熔断、重试预算。
- 推荐 `stale-while-revalidate`：
  - 前台先返回本地缓存结果
  - 后台刷新远端结果
- 任何在线失败都必须回退到离线可用结果。

建议可配置项：

- 全局是否启用在线补全
- 缓存 TTL 与刷新策略
- 强制离线模式（配置或命令参数）

## 6. 建议的可插拔接口（下一阶段实现）

```go
type CompletionContext struct {
    Command  string
    Token    string
    Segment  string // platform | name | version
    LastFlag string // --source | --index ...
    Offline  bool
}

type CandidateProvider interface {
    Name() string
    Priority() int
    Provide(ctx CompletionContext) ([]CompletionCandidate, error)
}
```

推荐执行流水线：

1. 按 command/flag/segment 路由。
2. 按优先级调用 Provider。
3. 合并并按 `Value` 去重。
4. 打分排序。
5. 限制最大候选数。
6. 统一由 `PrintCandidates` 输出。

## 7. 评审波次结论（F1~F4）

- F1（计划一致性）：通过，回退目标与约束满足。
- F2（代码质量）：无阻断问题；建议增强 `info` 的 flag 检测健壮性。
- F3（手工 QA）：命令矩阵通过；直接调用 `--generate-shell-completion` 与真实 shell 交互存在协议差异，验证应以真实 shell 效果为准。
- F4（范围忠实度）：整体符合“移除动态聚合、保留框架”的主目标。

## 8. 下一轮最小可执行清单

1. 先补 helper 单测（不改行为）。
2. 统一 `info/search` 的 flag 上下文识别策略。
3. 先落地 Provider 接口，但只接入 static provider（确保行为不变）。
4. 再新增一个离线 provider（本地索引）并加超时/降级。
5. 跑完整四 shell 冒烟矩阵后再考虑联网 provider。
