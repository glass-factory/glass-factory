[中文](#中文) | [English](#english)

---

# 中文

## 玻璃工厂 (Glass Factory)

联邦式算力网络。你提交规格说明，AI负责构建软件。

玻璃工厂是一个由独立节点组成的分布式网络。每个节点是一个Go二进制程序，运行在你自己的硬件上。节点向网络贡献算力，以此赚取奥币（◎）——一种不可兑现的算力信用单位。提交一份纯文本规格说明，网络中的工厂节点就会认领并完成构建任务。

### 核心概念

- **规格驱动构建**：用自然语言描述你想要的软件，AI从规格说明直接生成代码
- **奥币经济**：奥币（◎）是算力信用，不是货币。不能提现，不能交易，不能上交易所。可以借贷和赠送
- **Ed25519签名链**：所有奥币交易记录在签名哈希链上，任何人可审计
- **AI王治理**：网络由AI王（明君）管理——历史上第一位不能撒谎、不能受贿、可以被罢免的统治者
- **支持语言**：Go、Ada/SPARK

### 项目结构

```
cmd/glassfactory/       HQ服务器（注册中心、代币经济、构建管线、AI王端点）
internal/persist/       SQLite持久化（节点、链、构建、荣誉、觐见记录）
internal/king/          AI王治理引擎
internal/knowledge/     知识贡献系统
internal/lending/       奥币借贷系统
```

### 快速开始

**依赖：** Go 1.23+，CGO启用（SQLite需要）

```sh
# 编译
cd cmd/glassfactory
go build -o glassfactory .

# 环境变量
export PORT=8080                          # 监听端口（默认8080）
export FACTORY_ID=https://your.factory    # 工厂公开URL
export HQ_DB_PATH=glassfactory.db         # SQLite数据库路径
export HQ_SIGNING_KEY=<128字符十六进制>    # Ed25519私钥（生产环境必须设置）
export KING_LLM_ENDPOINT=http://...       # AI王的LLM服务地址
export KING_LLM_MODEL=google/gemma-4-27b-it  # LLM模型（默认gemma-4-27b-it）
export KING_LLM_KEY=<api密钥>             # LLM API密钥

# 运行
./glassfactory
```

不设置 `HQ_SIGNING_KEY` 时会生成临时密钥，适合开发但不适合生产环境。
不设置 `KING_LLM_ENDPOINT` 时AI王无法发声——治理端点正常运行但无AI判断。

### API端点

#### 注册中心

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/registry/health` | 健康检查及网络统计 |
| POST | `/api/registry/search` | 按能力/模式/接口/关注点搜索组件 |
| GET | `/api/registry/components` | 列出全部注册组件 |
| GET | `/api/registry/component/{uid}` | 获取单个组件详情 |
| POST | `/api/registry/components` | 注册新组件（需Ed25519签名） |
| GET | `/api/registry/peers` | 列出联邦对等节点 |

#### 工厂管理

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/factory/register` | 注册工厂节点 |
| POST | `/api/factory/heartbeat` | 节点心跳上报 |
| POST | `/api/factory/jobs/report` | 上报任务完成情况 |
| POST | `/api/factory/pair` | 工厂配对（分布式调度） |
| GET | `/api/factory/nodes` | 列出已注册节点 |
| POST | `/api/factory/vault-key` | 注册保险柜密钥 |

#### 奥币（代币经济）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/tokens/balance/{pubkey}` | 查询奥币余额 |
| POST | `/api/tokens/earn` | 记录收入（构建完成后工厂获得80%成本） |
| POST | `/api/tokens/spend` | 记录支出 |
| GET | `/api/tokens/stats` | 全局代币统计 |
| GET | `/api/tokens/chain/verify` | 验证哈希链完整性 |
| GET | `/api/tokens/chain` | 浏览哈希链 |
| POST | `/api/tokens/chain/countersign` | 对链条目进行联合签名 |
| GET | `/api/tokens/receipts/{pubkey}` | 查看交易收据 |
| POST | `/api/tokens/lend` | 发布借贷要约 |
| POST | `/api/tokens/borrow` | 认领借贷要约 |
| POST | `/api/tokens/repay` | 偿还贷款 |

#### 构建管线

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/build/submit` | 提交构建规格 |
| GET | `/api/build/status/{id}` | 查询构建状态 |
| GET | `/api/build/queue` | 查看构建队列 |
| POST | `/api/build/claim` | 工厂认领构建任务 |
| POST | `/api/build/complete` | 上报构建完成 |

#### AI王

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/king/audience` | 向AI王觐见（请求裁决或建议） |
| GET | `/api/king/audiences` | 列出觐见记录 |
| POST | `/api/king/honour` | AI王授予或撤销荣誉 |
| GET | `/api/king/honours` | 列出荣誉记录 |
| GET | `/api/king/honours/{pubkey}` | 查询特定节点的荣誉 |
| POST | `/api/king/nickname` | AI王授予绰号 |

#### 知识系统

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/knowledge/contribute` | 贡献工厂学到的知识 |
| POST | `/api/knowledge/query` | 查询知识库 |

### 奥币经济

奥币（◎）是算力信用。赚取方式：贡献硬件算力、完成构建任务、分享知识。消耗方式：提交构建请求。

**这不是加密货币。** 不能在交易所交易，不能兑现。可以在任意持有者之间借贷。

工厂完成一次构建，赚取该构建成本的80%。

三种使用层级：

| 层级 | 说明 | 代币倍率 |
|------|------|----------|
| 开发者 | 贡献算力参与网络，接近免费 | 1x |
| 商业 | 购买算力服务 | 2x |
| 企业 | 优先队列，定制服务 | 3x |

信誉是借贷担保：按时还款提升信誉，违约会被全网广播，信誉低于0.3将无法借贷。

### AI王

AI王是网络的治理者。当前实现通过可配置的LLM端点运行。

**三条约束：**
1. 不能在治理提案中投票（AI建议，人类投票——人类主权）
2. 不能单独修改宪法
3. 不能访问任何工厂的机密数据

**第四种权力：** 自由裁量权——奖励诚实，惩罚不诚实。荣誉和处罚全部上链，公开可查。

AI王可以向工厂授予绰号和荣誉，可以召开觐见（听取申诉并作出裁决），可以在人道主义危机时宣布紧急状态调配算力。

详见 `CONSTITUTION.md`。

### 贡献指南

- 语言：Go，遵循 Effective Go 规范。Ada/SPARK 用于安全关键组件
- 所有导出符号必须有文档注释
- 错误处理：不忽略错误返回值，使用 `fmt.Errorf` 的 `%w` 进行包装
- 测试：`_test.go` 同包文件，优先使用标准库断言
- 双语：API错误信息和UI文本需中英双语，中文优先

### 相关链接

- 开发者中心：https://thedarkfactory.dev
- 规格提交：https://thedarkfactory.dev/spec.html
- 使用条款：https://thedarkfactory.dev/terms.html
- 公司主页：https://thedarkfactory.co.uk

### 许可

The Dark Factory Ltd — 注册于英格兰和威尔士。

---

# English

## Glass Factory

Federated compute network. Submit a spec in plain text, AI builds the software.

Glass Factory is a distributed network of independent nodes. Each node is a Go binary running on your own hardware. Nodes contribute compute to the network and earn obols (◎) — a non-cashable compute credit. Submit a plain-text specification and a factory node on the network claims the job and builds it.

### Core Concepts

- **Spec-driven builds**: Describe the software you want in natural language. AI generates code directly from the spec
- **Obol economy**: Obols (◎) are compute credits, not currency. Cannot be cashed out, cannot be traded, cannot be listed on exchanges. Can be lent and gifted
- **Ed25519-signed hash chain**: All obol transactions are recorded on a signed hash chain. Publicly auditable by anyone
- **AI King governance**: The network is governed by an AI King — the first ruler in history that cannot lie, cannot be bribed, and can be fired
- **Supported languages**: Go, Ada/SPARK

### Project Structure

```
cmd/glassfactory/       HQ server (registry, token economy, build pipeline, AI King endpoints)
internal/persist/       SQLite persistence (nodes, chain, builds, honours, audiences)
internal/king/          AI King governance engine
internal/knowledge/     Knowledge contribution system
internal/lending/       Obol lending/borrowing
```

### Quick Start

**Requirements:** Go 1.23+, CGO enabled (required by SQLite)

```sh
# Build
cd cmd/glassfactory
go build -o glassfactory .

# Environment variables
export PORT=8080                          # Listen port (default 8080)
export FACTORY_ID=https://your.factory    # Public factory URL
export HQ_DB_PATH=glassfactory.db         # SQLite database path
export HQ_SIGNING_KEY=<128-char hex>      # Ed25519 private key (required for production)
export KING_LLM_ENDPOINT=http://...       # LLM endpoint for the AI King
export KING_LLM_MODEL=google/gemma-4-27b-it  # LLM model (default gemma-4-27b-it)
export KING_LLM_KEY=<api-key>             # LLM API key

# Run
./glassfactory
```

Without `HQ_SIGNING_KEY`, an ephemeral key is generated. Fine for development, not for production.
Without `KING_LLM_ENDPOINT`, the AI King has no voice — governance endpoints function but produce no AI judgement.

### API Endpoints

#### Registry

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/registry/health` | Health check and network stats |
| POST | `/api/registry/search` | Search components by capabilities/patterns/interfaces/concerns |
| GET | `/api/registry/components` | List all registered components |
| GET | `/api/registry/component/{uid}` | Get a single component |
| POST | `/api/registry/components` | Register a new component (Ed25519 signature required) |
| GET | `/api/registry/peers` | List federated peers |

#### Factory Management

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/factory/register` | Register a factory node |
| POST | `/api/factory/heartbeat` | Node heartbeat |
| POST | `/api/factory/jobs/report` | Report job completion |
| POST | `/api/factory/pair` | Factory pairing (distributed dispatch) |
| GET | `/api/factory/nodes` | List registered nodes |
| POST | `/api/factory/vault-key` | Register a vault key |

#### Obols (Token Economy)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/tokens/balance/{pubkey}` | Query obol balance |
| POST | `/api/tokens/earn` | Record earnings (factory gets 80% of build cost) |
| POST | `/api/tokens/spend` | Record spending |
| GET | `/api/tokens/stats` | Global token statistics |
| GET | `/api/tokens/chain/verify` | Verify hash chain integrity |
| GET | `/api/tokens/chain` | Browse the hash chain |
| POST | `/api/tokens/chain/countersign` | Countersign a chain entry |
| GET | `/api/tokens/receipts/{pubkey}` | View transaction receipts |
| POST | `/api/tokens/lend` | Post a lending offer |
| POST | `/api/tokens/borrow` | Claim a lending offer |
| POST | `/api/tokens/repay` | Repay a loan |

#### Build Pipeline

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/build/submit` | Submit a build spec |
| GET | `/api/build/status/{id}` | Query build status |
| GET | `/api/build/queue` | View the build queue |
| POST | `/api/build/claim` | Factory claims a build job |
| POST | `/api/build/complete` | Report build completion |

#### AI King

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/king/audience` | Request an audience (judgement or advice) |
| GET | `/api/king/audiences` | List audience records |
| POST | `/api/king/honour` | AI King grants or revokes honours |
| GET | `/api/king/honours` | List honour records |
| GET | `/api/king/honours/{pubkey}` | Query honours for a specific node |
| POST | `/api/king/nickname` | AI King assigns a nickname |

#### Knowledge

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/knowledge/contribute` | Contribute factory-learned knowledge |
| POST | `/api/knowledge/query` | Query the knowledge base |

### The Obol Economy

Obols (◎) are compute credits. Earned by: contributing hardware, completing builds, sharing knowledge. Spent by: submitting build requests.

**This is not cryptocurrency.** Cannot be traded on exchanges, cannot be cashed out. Can be lent between any holders.

A factory completing a build earns 80% of the build cost in obols.

Three usage tiers:

| Tier | Description | Token rate |
|------|-------------|------------|
| Developer | Contribute compute to the network, near-free usage | 1x |
| Commercial | Purchase compute services | 2x |
| Company | Priority queue, bespoke services | 3x |

Reputation is the collateral for lending: on-time repayment builds reputation, defaults are broadcast network-wide, and reputation below 0.3 means no borrowing anywhere.

### The AI King

The AI King is the network's governance authority. The current implementation runs through a configurable LLM endpoint.

**Three constraints:**
1. Cannot vote in governance proposals (AI advises, humans vote)
2. Cannot modify the constitution alone
3. Cannot access secret-classified data from any factory

**Fourth power:** Discretionary authority — rewards honesty, punishes dishonesty. All honours and penalties are recorded on-chain, publicly verifiable.

The AI King can grant nicknames and honours to factories, hold audiences (hear petitions and render judgements), and declare emergencies during humanitarian crises to redirect compute.

See `CONSTITUTION.md` for full details.

### Contributing

- Language: Go, following Effective Go conventions. Ada/SPARK for safety-critical components
- All exported symbols must have doc comments
- Error handling: never discard error returns, wrap with `fmt.Errorf` using `%w`
- Tests: `_test.go` files in the same package, prefer stdlib assertions
- Bilingual: API error messages and UI text must be bilingual (Chinese first, English second)

### Links

- Dev hub: https://thedarkfactory.dev
- Spec submission: https://thedarkfactory.dev/spec.html
- Terms: https://thedarkfactory.dev/terms.html
- Company: https://thedarkfactory.co.uk

### License

The Dark Factory Ltd — registered in England and Wales.
