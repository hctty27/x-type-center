---
name: type-registry
description: 跨项目全局类型查询、申请和校验。新增或修改 ActivityType、RankType、SceneType、ItemAddType、ItemRemoveType、GameServerCode、ClientResCode 等全局类型时使用。
---

# Type Registry

## 强制规则

任何新增全局类型前：

1. 禁止根据项目代码中的现有最大值自行递增。
2. 项目里的类型名称如果不是已确认的标准 Namespace code，必须先调用 `resolve`，禁止凭中文名称、类名或上下文自行猜测标准 Namespace。
3. 再搜索 Registry，确认不存在相同或等价业务定义。
4. 查看解析后的标准 Namespace 状态。
5. 仅使用 Registry `allocate` 返回的 value。
6. 修改代码后执行 `validate`。

Registry 不使用 Token，不要向用户索要 Token，也不要添加 Authorization Header。

## 使用方式

本 Skill 自带零第三方依赖客户端：

```text
scripts/type_registry.py
config.json
```

服务地址直接从本 Skill 目录下的 `config.json` 读取。从 X Type Center 页面下载的技能包会自动写入当前服务的公开地址，通常无需手动修改 `baseUrl`。如果技能包是手工复制或跨环境使用，再检查 `config.json` 中的 `baseUrl` 是否可从当前机器访问。

把整个 `type-registry` Skill 目录复制到项目或 OpenCode 全局 Skill 目录后即可使用，不要求项目安装 `type-registry` CLI，也不要求配置环境变量。

执行命令时，先定位当前 `SKILL.md` 所在目录，再使用该目录下的脚本，不要假设 Skill 一定安装在某个固定绝对路径。

### Namespace

```bash
python3 <skill-dir>/scripts/type_registry.py namespaces
python3 <skill-dir>/scripts/type_registry.py status RankType
```

### Namespace 解析

项目术语可能和 Registry 标准名称不同，例如项目里叫“商城类型”，Registry 标准 Namespace 可能是 `ShopType（商店类型）`。遇到中文名称、项目类名、简称或其他非标准 code 时，先解析：

```bash
python3 <skill-dir>/scripts/type_registry.py resolve "商城类型"
python3 <skill-dir>/scripts/type_registry.py resolve "ShopType"
```

`resolve` 会按标准 Namespace code、标准名称、已登记别名进行精确解析。返回 `matched=false` 时不得自行猜测 Namespace，应报告无法确定或让用户先在 X Type Center 中补充别名。

### 搜索

使用业务中文名和常量名分别搜索一次：

```bash
python3 <skill-dir>/scripts/type_registry.py search --namespace RankType "混沌灵域"
python3 <skill-dir>/scripts/type_registry.py search --namespace RankType "CHAOS_REALM_RANK"
```

### 申请

只有 Namespace 是分配所必需的，其余元数据均可省略。默认申请 1 个值：

```bash
python3 <skill-dir>/scripts/type_registry.py allocate --namespace RankType
```

有业务信息时再附带可选字段：

```bash
python3 <skill-dir>/scripts/type_registry.py allocate \
  --namespace RankType \
  --project XH2 \
  --symbol CHAOS_REALM_RANK \
  --description "混沌灵域排行榜" \
  --requirement XH2-3124
```

需要一次申请多个值时使用 `--count`，范围为 1 到 100：

```bash
python3 <skill-dir>/scripts/type_registry.py allocate \
  --namespace ActivityType \
  --count 5 \
  --project XH2 \
  --description "混沌灵域活动类型"
```

批量申请规则：

1. `count > 1` 时不得传 `--symbol`。
2. 批量返回的 `values` 已经完成登记，直接使用这些值，不要因为 entry 的 symbol 为空而再次申请。
3. 如果需求需要多个具体业务常量，将返回的 `values` 按需求顺序写入代码。
4. 批量申请后的校验不要传 `--symbol`，按 Namespace、value 和可选的 project 校验。

### 撤回

Registry 支持撤回误申请。撤回后 entry 会保留为 `REVOKED` 历史记录，但对应 value 会释放，后续 `allocate` 可以重新分配。不得因为看到已撤回 value 就在项目代码中手工复用，仍必须以 Registry 新一次 `allocate` 返回的 value 为准。撤回原因是可选字段，有明确上下文时建议填写。

只有在以下情况使用撤回：

1. 用户明确要求撤回。
2. 当前 AI 刚完成一次错误申请，并且能够确定本次返回的 `allocationId`。
3. 不得为了获取特定号码主动撤回；撤回只用于取消错误或不再需要的申请。

整批撤回优先使用本次申请返回的 `allocationId`：

```bash
python3 <skill-dir>/scripts/type_registry.py revoke \
  --allocation alloc_xxx \
  --requester hc
```

单条历史记录需要已知 entry id：

```bash
python3 <skill-dir>/scripts/type_registry.py revoke \
  --id 123 \
  --requester hc
```

撤回接口是幂等的；第一次撤回即释放 value，重复撤回同一条已 `REVOKED` 的记录不会产生额外副作用。

### 校验

单个申请且 Registry 中登记了 symbol 时：

```bash
python3 <skill-dir>/scripts/type_registry.py validate \
  --namespace RankType \
  --value 447 \
  --symbol CHAOS_REALM_RANK \
  --project XH2
```

批量申请的值因为 Registry entry 不登记 symbol，应逐个这样校验：

```bash
python3 <skill-dir>/scripts/type_registry.py validate \
  --namespace ActivityType \
  --value 501 \
  --project XH2
```

## AI 工作流

当需求需要新增全局类型时：

1. 从目标常量类或业务上下文提取 Namespace 候选名称。
2. 如果候选不是已确认的标准 Namespace code，调用 `resolve`；别名命中后只使用返回的标准 `namespace.code` 进行后续操作。
3. 使用业务中文名、英文常量名分别搜索，避免重复定义。
4. 已存在时优先复用，不得再次申请。
5. 不存在时查看标准 Namespace 状态。
6. 根据需求数量调用 `allocate` 原子申请值；单个申请默认 `count=1`，多个值使用 `--count`，并保留返回的 `allocationId`。
7. 仅将本次 `allocate` 返回的 value/values 写入项目代码，不得自行推算或二次申请替代。
8. 如果用户明确指出本次申请有误，可使用返回的 `allocationId` 撤回整批；撤回后号码进入可重新分配状态，但不得手工指定复用。
9. 完成修改后调用 `validate`；批量申请的值不传 symbol。
10. `resolve` 未命中或 Registry 不可访问时不得自行猜测 Namespace 或新值，应明确报告无法安全处理。
