# x-type-center

跨项目全局类型注册中心。用于统一管理 `ActivityType`、`RankType`、`SceneType`、`ItemAddType`、`ItemRemoveType`、各类 ServerCode 等全局数值类型，避免多人/多项目/AI 开发时根据代码最大值直接 `+1` 导致冲突。

## 架构

```text
AI Agent -> Skill bundled client -> HTTP API -> Go Service -> MySQL
Web UI -----------------------------> HTTP API -> Go Service -> MySQL
```

Registry 是唯一事实源，代码常量不是分配依据。

## 能力

- Namespace 列表、当前最大值、下一个可分配值
- Namespace 全局别名与标准名称解析
- 全局/按 Namespace 搜索
- 原子申请新类型
- 项目预留区间
- 类型校验
- Web Namespace 总览和申请页面
- Web 一键下载 AI Skill 包
- AI Skill + CLI 工作流
- 单 Go 二进制部署（server/migrate/version）
- Docker Compose 本地开发可选

## 并发分配

分配事务会锁定 `type_namespaces` 对应记录：

```sql
SELECT ... FROM type_namespaces WHERE code = ? FOR UPDATE;
```

同一个 Namespace 的并发申请被串行化。数据库同时使用：

```text
UNIQUE(namespace_id, active_value)
UNIQUE(namespace_id, active_symbol)
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

服务启动时自动执行幂等 DDL。旧库会先校验并迁移数据，再收敛为 3 张业务表；新库直接创建 3 表结构。撤回记录保留历史，但其 value/symbol 不再占用唯一键。

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
x-type-center version
```

Web 静态资源和数据库 migration SQL 都已编译进二进制，不要求服务器安装 Go、Node、Docker 或额外配置文件。

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

撤回会保留 `REVOKED` 历史记录，同时释放该 value；后续申请可以由 Registry 重新分配这个值。

撤回单条：

```bash
bin/type-registry revoke --id 123 --requester hc
```

撤回一次申请：

```bash
bin/type-registry revoke --allocation alloc_xxx --requester hc
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

项目字段可选。Web 端会从 `GET /api/v1/projects` 加载项目候选，同时允许直接输入新项目；当申请事务成功时，新项目会自动登记到 `projects` 表。已有 `type_entries` 和旧版 `reserved_ranges` 中的项目会在升级时自动回填到 `projects` 表。

申请成功后返回 `allocationId`。Web 页面支持从申请结果直接撤回整批，也支持在 Namespace entries 中撤回单条记录。撤回会把状态改为 `REVOKED`，记录撤回人、IP、可选原因和时间，并释放该 value/symbol 的唯一占用；Namespace 游标会回退到可复用位置。重复调用同一撤回接口保持幂等。

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

`TYPE_REGISTRY_PUBLIC_URL` 未配置时，服务端会回退到当前请求的协议和 Host。反向代理或 HTTPS 终止场景应显式配置 `TYPE_REGISTRY_PUBLIC_URL`，避免生成错误的 `http://` 地址。

客户端 IP 审计默认只使用 TCP 连接的远端地址，不信任 `X-Forwarded-For`。如果服务部署在 Nginx / LB 后面，可配置例如 `TYPE_REGISTRY_TRUSTED_PROXIES=127.0.0.1/32`；只有直接连接来源命中可信代理列表时，服务端才会从完整 `X-Forwarded-For` 链右侧向左跳过可信代理并选择客户端地址。可信代理范围应尽量精确，不要为了方便配置过大的网段。

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
| `TYPE_REGISTRY_TRUSTED_PROXIES` | 空；逗号分隔的可信反向代理 IP/CIDR，仅这些代理可提供可信 `X-Forwarded-For` |
| `TYPE_REGISTRY_READ_TIMEOUT` | `10s` |
| `TYPE_REGISTRY_WRITE_TIMEOUT` | `15s` |
| `TYPE_REGISTRY_IDLE_TIMEOUT` | `60s` |

## 数据模型

数据库最终只保留 3 张业务表：

```text
type_namespaces
type_entries
projects
```

### type_namespaces

每种全局类型一个 Namespace，并保存分配游标 `next_value`。

Namespace 别名和项目预留区间直接作为 JSON 保存：

- `aliases`：保存别名对象，兼容现有别名查询、创建和按 ID 删除接口。
- `reserved_ranges`：保存项目预留或全局阻塞区间。

旧版 `namespace_aliases` 和 `reserved_ranges` 表的数据会在升级时完整回填到这两个 JSON 字段，校验数量一致后才删除旧表。

### type_entries

已注册类型值，同时承载申请批次和撤回历史：

- `allocation_id`：同一次批量申请的所有 entry 使用相同批次号。
- `request_ip`：记录申请来源 IP。
- `revoked_by`、`revoke_ip`、`revoke_reason`、`revoked_at`：保存撤回信息。
- 状态支持 `ACTIVE`、`REVOKED`、`DEPRECATED`。
- `active_value` / `active_symbol` 是生成列：`REVOKED` 时为 `NULL`，因此撤回后 value/symbol 可由 Registry 再次分配；`DEPRECATED` 仍保持占用。
- 已撤回旧记录不会物理删除，所以同一个 Namespace/value 后续可能同时存在一条历史 `REVOKED` 记录和一条新的有效记录。

因此旧版 `type_allocations`、`type_allocation_entries`、`type_entry_revocations` 和 `audit_logs` 都不再需要独立表。

### projects

项目候选表。项目字段仍直接记录在 `type_entries.project`，申请新项目时自动 `INSERT IGNORE` 到该表，供 Web 搜索下拉框使用。

### 旧库自动升级

新版本启动或执行 `x-type-center migrate` 时：

```text
创建/补齐 3 表字段
↓
迁移 Namespace 别名与预留区间
↓
迁移 allocation_id / 撤回信息
↓
从 audit_logs 回填申请 IP / 撤回 IP
↓
回填 projects 并校验旧数据
↓
建立仅对非 REVOKED 生效的唯一索引
↓
重新计算 Namespace 可用游标
↓
删除 audit_logs、旧辅助表和 Excel/source 遗留字段
```

如果校验数量不一致，迁移会直接失败并保留旧表，不会继续执行删除。

## 服务端命令

```text
x-type-center server
x-type-center migrate
x-type-center version
```

`server` 会自动执行当前幂等 migration；`migrate` 适合部署阶段显式初始化数据库。

## 注意事项

- 已使用类型不要物理删除；误申请使用 `REVOKED`，正式下线使用 `DEPRECATED`。
- `REVOKED` value 只允许由 Registry 后续 `allocate` 自动复用，不要在项目代码中手工认领历史号码。
- API 本身不做应用层鉴权；生产环境建议仅暴露在公司内网/VPN，或由统一网关/SSO 控制访问范围。
