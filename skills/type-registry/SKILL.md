---
name: type-registry
description: 跨项目全局类型查询、申请和校验。新增或修改 ActivityType、RankType、SceneType、ItemAddType、ItemRemoveType、GameServerCode、ClientResCode 等全局类型时使用。
---

# Type Registry

## 强制规则

任何新增全局类型前：

1. 禁止根据项目代码中的现有最大值自行递增。
2. 先搜索 Registry，确认不存在相同或等价业务定义。
3. 再查看目标 Namespace 状态。
4. 仅使用 Registry `allocate` 返回的 value。
5. 修改代码后执行 `validate`。

Registry 地址从 `TYPE_REGISTRY_URL` 读取，鉴权 Token 从 `TYPE_REGISTRY_TOKEN` 读取。不要把 Token 写入代码、Skill 或提交到 Git。

## CLI

CLI 名称：`type-registry`。

### 搜索

```bash
type-registry search --namespace RankType "混沌灵域"
```

### 查看 Namespace

```bash
type-registry status RankType
```

### 申请

```bash
type-registry allocate \
  --namespace RankType \
  --project XH2 \
  --symbol CHAOS_REALM_RANK \
  --description "混沌灵域排行榜" \
  --requirement XH2-3124
```

### 校验

```bash
type-registry validate \
  --namespace RankType \
  --value 447 \
  --symbol CHAOS_REALM_RANK \
  --project XH2
```

## AI 工作流

当需求需要新增全局类型时：

1. 从目标常量类或业务上下文确定 Namespace。
2. 使用业务中文名、英文常量名分别搜索一次，避免重复定义。
3. 已存在时优先复用，不得再次申请。
4. 不存在时申请新值。
5. 将返回的 symbol/value 写入代码。
6. 完成修改后校验 Registry。
7. 若 CLI 或 Registry 不可用，不得自行猜测新值；明确告诉用户当前无法安全分配。
