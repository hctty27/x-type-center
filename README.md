# x-type-center

跨项目全局类型注册中心。用于统一管理 `ActivityType`、`RankType`、`SceneType`、`ItemAddType`、`ItemRemoveType`、各类 ServerCode 等全局数值类型，避免多人/多项目/AI 开发时根据代码最大值直接 `+1` 导致冲突。

## 架构

```text
AI Agent -> Skill bundled client -> HTTP API -> Go Service -> MySQL
Web UI -----------------------------> HTTP API -> Go Service -> MySQL
Excel Import -> x-type-center import -------------> MySQL
```

Registry 是唯一事实源。Excel 仅用于历史数据首次导入；代码常量不是分配依据。

## 能力

- Namespace 列表、当前最大值、下一个可分配值
- Namespace 全局别名与标准名称解析
- 全局/按 Namespace 搜索
- 原子申请新类型
- 项目预留区间
- 历史 Excel 导入
- 类型校验
- Web Namespace 总览和申请页面
- Web 一键下载 AI Skill 包
- AI Skill + CLI 工作流
- 单 Go 二进制部署（server/import/migrate/version）
- Docker Compose 本地开发可选

## 并发分配

分配事务会锁定 `type_namespaces` 对应记录：

```sql
SELECT ... FROM type_namespaces WHERE code = ? FOR UPDATE;
```

同一个 Namespace 的并发申请被串行化。数据库同时使用：

```text
UNIQUE(namespace_id, value)
UNIQUE(namespace_id, symbol)
```

做最终一致性兜底。

## 快速启动

### Docker Compose

```bash
docker compose up -d --build
```

访问：

```text
http://127.0.0.1:8080
```

健康检查：

```bash
curl http://127.0.0.1:8080/healthz
```

### 本地启动

先启动 MySQL，并创建数据库：

```sql
CREATE DATABASE x_type_center CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
```

环境变量：

```bash
export TYPE_REGISTRY_DSN='type_center:type_center@tcp(127.0.0.1:3306)/x_type_center?charset=utf8mb4&parseTime=true&loc=UTC'
go run ./cmd/x-type-center server
```

服务启动时自动执行幂等 DDL。

## 单二进制部署

编译当前平台：

```bash
make build VERSION=v0.1.0
```

产物：

```text
bin/x-type-center
```

交叉编译 Linux amd64：

```bash
make build-linux VERSION=v0.1.0
```

产物：

```text
dist/x-type-center-linux-amd64
```

服务端只需要这个二进制和可访问的 MySQL：

```bash
x-type-center server
x-type-center migrate
x-type-center import --file /path/to/types.xlsx
x-type-center version
```

Web 静态资源、数据库 migration SQL 和默认 Excel mapping 都已编译进二进制，不要求服务器安装 Go、Node、Docker 或额外配置文件。

### systemd

仓库提供：

```text
deploy/systemd/x-type-center.service
deploy/systemd/x-type-center.env.example
```

典型部署：

```bash
sudo install -m 0755 dist/x-type-center-linux-amd64 /usr/local/bin/x-type-center
sudo useradd --system --no-create-home --shell /usr/sbin/nologin x-type-center || true
sudo mkdir -p /etc/x-type-center /var/lib/x-type-center
sudo cp deploy/systemd/x-type-center.env.example /etc/x-type-center/x-type-center.env
sudo cp /path/to/type-registry-skill.zip /var/lib/x-type-center/type-registry-skill.zip
sudo cp deploy/systemd/x-type-center.service /etc/systemd/system/x-type-center.service

sudo systemctl daemon-reload
sudo systemctl enable --now x-type-center
sudo systemctl status x-type-center
```

日志：

```bash
journalctl -u x-type-center -f
```

## 导入历史 Excel

默认 mapping 已通过 `go:embed` 编译进 `x-type-center`，按原始类型表配置 5 个数据 Sheet、59 个类型列。

导入器按表头文字定位列，不依赖 `A/B/C` 等固定列号，因此插入普通列不会导致映射错位。

```bash
go run ./cmd/x-type-center import --file /path/to/types.xlsx
```

如需临时覆盖 mapping，仍可显式传入：

```bash
x-type-center import --file /path/to/types.xlsx --mapping /path/to/mapping.json
```

或：

```bash
make import FILE=/path/to/types.xlsx
```

特殊值：

```text
5201-5700 SLG预留
15500-15800 SLG预留
1475-1514
```

会作为 `reserved_ranges` 导入。带 `SLG预留` 的区间只允许 SLG 项目优先分配；没有项目名的纯数字区间会作为全局阻塞区间，自动分配时跳过。

`SLGCode` Sheet 中的历史值自动记录 `project=SLG`。

## CLI

编译：

```bash
make cli
export TYPE_REGISTRY_URL=http://127.0.0.1:8080
```

### Namespace

```bash
bin/type-registry namespaces
bin/type-registry status RankType
```

### Namespace 别名解析

```bash
bin/type-registry resolve '商城类型'
```

标准 code、标准名称或已登记别名都会解析到唯一的标准 Namespace。

### 搜索

```bash
bin/type-registry search --namespace RankType '混沌灵域'
```

### 申请

只有 Namespace 必需，其余元数据可选：

```bash
bin/type-registry allocate --namespace RankType
```

也可以补充项目、常量名、描述等信息：

```bash
bin/type-registry allocate \
  --namespace RankType \
  --project XH2 \
  --symbol CHAOS_REALM_RANK \
  --description '混沌灵域排行榜' \
  --requirement XH2-3124
```

### 撤回

撤回不会释放类型值，记录会变为 `REVOKED`，该 value 永久不可重新分配。

撤回单条：

```bash
bin/type-registry revoke --id 123 --reason '登记错误' --requester hc
```

撤回一次申请：

```bash
bin/type-registry revoke --allocation alloc_xxx --reason 'Namespace 选择错误' --requester hc
```

### 校验

```bash
bin/type-registry validate \
  --namespace RankType \
  --value 447 \
  --symbol CHAOS_REALM_RANK \
  --project XH2
```

## HTTP API

API 不使用应用层 Token。Web、Skill 和 CLI 都直接调用同一套接口。

主要接口：

```text
GET  /healthz
GET  /api/v1/namespaces
GET  /api/v1/projects
GET  /api/v1/namespaces/resolve?q=商城类型
GET  /api/v1/namespaces/{code}
GET  /api/v1/namespaces/{code}/aliases
POST /api/v1/namespaces/{code}/aliases
DELETE /api/v1/namespaces/{code}/aliases/{id}
GET  /api/v1/types/search?q=keyword&namespace=RankType&project=XH2&page=1&pageSize=10
GET  /api/v1/skill-package
POST /api/v1/types/allocate
POST /api/v1/types/allocate-batch
POST /api/v1/types/{id}/revoke
POST /api/v1/allocations/{allocationId}/revoke
POST /api/v1/types/validate
```

项目字段可选。Web 端会从 `GET /api/v1/projects` 加载项目候选，同时允许直接输入新项目；当申请事务成功时，新项目会自动登记到 `projects` 表。已有 `type_entries` 和 `reserved_ranges` 中的项目会在迁移时自动回填到项目表。

申请成功后返回 `allocationId`。Web 页面支持从申请结果直接撤回整批，也支持在 Namespace entries 中撤回单条记录。撤回只把状态改为 `REVOKED` 并记录撤回人、原因和时间，不回退 Namespace 游标，也不重新利用旧 value；重复调用同一撤回接口保持幂等。

申请示例：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/types/allocate \
  -H 'Content-Type: application/json' \
  -d '{
    "namespace":"RankType",
    "project":"XH2",
    "symbol":"CHAOS_REALM_RANK",
    "description":"混沌灵域排行榜",
    "requirement":"XH2-3124",
    "requester":"cheng"
  }'
```

## AI Skill

Skill 目录：

```text
skills/type-registry/
├── SKILL.md
├── config.json
└── scripts/
    └── type_registry.py
```

核心规则：

```text
resolve -> search -> status -> allocate -> 修改代码 -> validate
```

禁止 AI 根据项目常量类里的当前最大值自行递增；项目术语不是标准 Namespace code 时必须先通过 Registry resolve，不能凭名称自行猜测。

### 零配置接入项目

把整个 `skills/type-registry` 目录复制到目标项目支持的 Skill 目录即可。Skill 自带 Python 标准库 HTTP 客户端：

- 不需要 MCP。
- 不需要 Token。
- 不需要安装 `type-registry` CLI。
- 不需要项目配置环境变量。
- Registry 地址从 Skill 自带的 `config.json` 读取。

开发环境默认：

```json
{
  "baseUrl": "http://127.0.0.1:8080",
  "timeoutSeconds": 10
}
```

正式在多个项目分发前，把 `baseUrl` 改成公司内网统一的 X Type Center 地址。之后所有项目只需要复制同一份 Skill 目录。

### Web 下载技能包

页面右上角提供“下载技能包”按钮。服务端从配置的本地模板 ZIP 读取 Skill，在下载时实时把 `type-registry/config.json` 中的 `baseUrl` 改成当前部署地址：

```text
GET /api/v1/skill-package
```

推荐生产配置：

```bash
TYPE_REGISTRY_SKILL_PACKAGE_PATH=/var/lib/x-type-center/type-registry-skill.zip
TYPE_REGISTRY_PUBLIC_URL=https://x-type-center.internal
```

下载后的配置会自动变成：

```json
{
  "baseUrl": "https://x-type-center.internal",
  "timeoutSeconds": 10
}
```

`TYPE_REGISTRY_PUBLIC_URL` 未配置时，服务端会回退到当前请求的协议和 Host。反向代理或 HTTPS 终止场景应显式配置 `TYPE_REGISTRY_PUBLIC_URL`，避免生成错误的 `http://` 地址。服务端不会自动信任 `X-Forwarded-*` 请求头。

未配置 Skill 路径、文件不存在或 ZIP 内缺少 `type-registry/config.json` 时，页面按钮会不可用或下载失败。

本地生成 Skill ZIP 模板：

```bash
make skill-package
```

每次 `main` 自动 Release 也会额外上传：

```text
type-registry-skill.zip
type-registry-skill.zip.sha256
```

它们是独立 Release 附件，不会放进只包含 `x-type-center` 二进制的 Linux 服务压缩包。

## 配置

| 环境变量 | 默认值 |
|---|---|
| `TYPE_REGISTRY_ADDR` | `:8080` |
| `TYPE_REGISTRY_DSN` | 本地 MySQL DSN |
| `TYPE_REGISTRY_SKILL_PACKAGE_PATH` | 空；Skill ZIP 模板本地绝对路径 |
| `TYPE_REGISTRY_PUBLIC_URL` | 空；下载 Skill 时注入的服务地址 |
| `TYPE_REGISTRY_READ_TIMEOUT` | `10s` |
| `TYPE_REGISTRY_WRITE_TIMEOUT` | `15s` |
| `TYPE_REGISTRY_IDLE_TIMEOUT` | `60s` |

## 数据模型

### type_namespaces

每种全局类型一个 Namespace，并保存分配游标 `next_value`。

### namespace_aliases

Namespace 的全局替代名称。别名全局唯一，用于把“商城类型”“MallType”等项目术语确定性解析到标准 Namespace；第一版不区分 project。

### type_entries

已注册类型值。新申请时除 Namespace 外的元数据均可为空；未填写 `symbol` 时存储为 `NULL`，避免空字符串触发 Namespace 内 symbol 唯一约束冲突。状态支持 `ACTIVE`、`REVOKED`、`DEPRECATED`；只要 value 曾登记过，就永久视为占用。

### type_allocations / type_allocation_entries

记录一次申请及其包含的 entry，用于安全地撤回整批申请。每次新申请都会生成唯一 `allocationId`。

### type_entry_revocations

记录撤回人、撤回原因和撤回时间。撤回不物理删除 `type_entries`。

### reserved_ranges

项目预留或全局阻塞区间。

- `project='SLG'`：SLG 可优先从该区间申请，其他项目跳过。
- `project=''`：所有自动分配都跳过。

### audit_logs

记录 Registry 写操作，便于追查谁申请了什么值。

## 服务端命令

```text
x-type-center server
x-type-center import --file <types.xlsx> [--mapping <mapping.json>]
x-type-center migrate
x-type-center version
```

其中 `server`、`import` 会自动执行当前幂等 migration；`migrate` 适合部署阶段显式初始化数据库。

## 注意事项

- 已使用类型不要物理删除；误申请使用 `REVOKED`，正式下线后续使用 `DEPRECATED`。
- Excel 导入应先在测试库执行并检查报告，再切换 Registry 为唯一写入口。
- API 本身不做应用层鉴权；生产环境建议仅暴露在公司内网/VPN，或由统一网关/SSO 控制访问范围。
