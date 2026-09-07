# x-type-center

跨项目全局类型注册中心。用于统一管理 `ActivityType`、`RankType`、`SceneType`、`ItemAddType`、`ItemRemoveType`、各类 ServerCode 等全局数值类型，避免多人/多项目/AI 开发时根据代码最大值直接 `+1` 导致冲突。

## 架构

```text
AI Agent -> Skill -> type-registry CLI -> HTTP API -> Go Service -> MySQL
Web UI -------------------------------> HTTP API -> Go Service -> MySQL
Excel Import -> x-type-center import -------------> MySQL
```

Registry 是唯一事实源。Excel 仅用于历史数据首次导入；代码常量不是分配依据。

## 能力

- Namespace 列表、当前最大值、下一个可分配值
- 全局/按 Namespace 搜索
- 原子申请新类型
- 项目预留区间
- 历史 Excel 导入
- 类型校验
- Web 查询和申请页面
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
export TYPE_REGISTRY_TOKEN='replace-me'
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
export TYPE_REGISTRY_TOKEN='replace-me'
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
sudo mkdir -p /etc/x-type-center
sudo cp deploy/systemd/x-type-center.env.example /etc/x-type-center/x-type-center.env
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
export TYPE_REGISTRY_TOKEN='replace-me'
```

### Namespace

```bash
bin/type-registry namespaces
bin/type-registry status RankType
```

### 搜索

```bash
bin/type-registry search --namespace RankType '混沌灵域'
```

### 申请

```bash
bin/type-registry allocate \
  --namespace RankType \
  --project XH2 \
  --symbol CHAOS_REALM_RANK \
  --description '混沌灵域排行榜' \
  --requirement XH2-3124
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

读取接口默认无需 Token；写接口在配置 `TYPE_REGISTRY_TOKEN` 后要求：

```http
Authorization: Bearer <token>
```

主要接口：

```text
GET  /healthz
GET  /api/v1/namespaces
GET  /api/v1/namespaces/{code}
GET  /api/v1/types/search?q=keyword&namespace=RankType&project=XH2&page=1&pageSize=10
POST /api/v1/types/allocate
POST /api/v1/types/validate
```

申请示例：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/types/allocate \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TYPE_REGISTRY_TOKEN" \
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

Skill 位于：

```text
skills/type-registry/SKILL.md
```

核心规则：

```text
search -> status -> allocate -> 修改代码 -> validate
```

禁止 AI 根据项目常量类里的当前最大值自行递增。

Skill 不保存 Token。客户端通过：

```text
TYPE_REGISTRY_URL
TYPE_REGISTRY_TOKEN
```

访问服务。

## 配置

| 环境变量 | 默认值 |
|---|---|
| `TYPE_REGISTRY_ADDR` | `:8080` |
| `TYPE_REGISTRY_DSN` | 本地 MySQL DSN |
| `TYPE_REGISTRY_TOKEN` | 空，写接口不鉴权；生产必须配置 |
| `TYPE_REGISTRY_READ_TIMEOUT` | `10s` |
| `TYPE_REGISTRY_WRITE_TIMEOUT` | `15s` |
| `TYPE_REGISTRY_IDLE_TIMEOUT` | `60s` |

## 数据模型

### type_namespaces

每种全局类型一个 Namespace，并保存分配游标 `next_value`。

### type_entries

已注册类型值。历史 Excel 数据允许 `symbol=NULL`；Registry 新申请必须提供 `symbol`。

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

- 已使用类型不要物理删除，后续应增加 `DEPRECATED` 管理入口。
- Excel 导入应先在测试库执行并检查报告，再切换 Registry 为唯一写入口。
- 生产环境必须配置 API Token，并建议放在公司网关/SSO 后。
- `TYPE_REGISTRY_TOKEN` 不要提交到仓库、Skill 或项目代码。
