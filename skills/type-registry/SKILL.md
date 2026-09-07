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

Registry 不使用 Token，不要向用户索要 Token，也不要添加 Authorization Header。

## 使用方式

本 Skill 自带零第三方依赖客户端：

```text
scripts/type_registry.py
config.json
```

服务地址直接从本 Skill 目录下的 `config.json` 读取。把整个 `type-registry` Skill 目录复制到项目后即可使用，不要求项目安装 `type-registry` CLI，也不要求配置环境变量。

执行命令时，先定位当前 `SKILL.md` 所在目录，再使用该目录下的脚本，不要假设 Skill 一定安装在某个固定绝对路径。

### Namespace

```bash
python3 <skill-dir>/scripts/type_registry.py namespaces
python3 <skill-dir>/scripts/type_registry.py status RankType
```

### 搜索

使用业务中文名和常量名分别搜索一次：

```bash
python3 <skill-dir>/scripts/type_registry.py search --namespace RankType "混沌灵域"
python3 <skill-dir>/scripts/type_registry.py search --namespace RankType "CHAOS_REALM_RANK"
```

### 申请

```bash
python3 <skill-dir>/scripts/type_registry.py allocate \
  --namespace RankType \
  --project XH2 \
  --symbol CHAOS_REALM_RANK \
  --description "混沌灵域排行榜" \
  --requirement XH2-3124
```

### 校验

```bash
python3 <skill-dir>/scripts/type_registry.py validate \
  --namespace RankType \
  --value 447 \
  --symbol CHAOS_REALM_RANK \
  --project XH2
```

## AI 工作流

当需求需要新增全局类型时：

1. 从目标常量类或业务上下文确定 Namespace。
2. 使用业务中文名、英文常量名分别搜索，避免重复定义。
3. 已存在时优先复用，不得再次申请。
4. 不存在时查看 Namespace 状态。
5. 调用 `allocate` 原子申请新值。
6. 将返回的 symbol/value 写入项目代码。
7. 完成修改后调用 `validate`。
8. Registry 不可访问时不得自行猜测新值，应明确报告无法安全分配。
