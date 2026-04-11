# Glass Factory Protocol Specification v0.1

**玻璃工厂协议规范**

The Glass Factory is a federated, open component registry and maker network.
This document defines the protocol — not the implementation. Any language,
any platform, any factory can participate by speaking this protocol.

玻璃工厂是一个联邦式的开放组件注册表和制造者网络。
本文档定义的是协议，而非实现。任何语言、任何平台、任何工厂
都可以通过遵循此协议参与其中。

---

## 1. Identity

### 1.1 Maker Identity

A maker is identified by an Ed25519 public key. No accounts, no emails, no PII required.

```
public_key:  32 bytes, hex-encoded (64 characters)
handle:      UTF-8 string, 1-64 characters, unique per registry
```

Makers MAY provide an encrypted recovery email. Registries MUST NOT store
plaintext emails. AES-256-GCM encryption is RECOMMENDED.

### 1.2 Factory Identity

A factory is identified by its registry URL and Ed25519 public key.

```
factory_url:  HTTPS URL of the registry API root
factory_key:  Ed25519 public key (hex-encoded)
```

### 1.3 Signatures

All mutations (registrations, votes, component publications) MUST be signed
by the actor's Ed25519 private key. The signing format is:

```
message = field1 | field2 | ... | fieldN    (pipe-delimited UTF-8)
signature = Ed25519.Sign(private_key, message)
encoding = hex(signature)                    (128 hex characters)
```

Verifiers MUST reject unsigned mutations and mutations from unknown keys.

---

## 2. Component Registry API

All endpoints accept and return `application/json`. All timestamps are ISO 8601 UTC.

### 2.1 Search Components

```
POST /api/registry/search

Request:
{
  "capabilities": ["serves-http", "handles-auth"],   // what it does
  "patterns":     ["middleware"],                      // architectural role
  "interfaces":   ["request-handler"],                 // what it exposes
  "concerns":     ["security"],                        // cross-cutting
  "language":     "go",                                // optional: filter by language
  "federate":     true                                 // optional: also search peers
}

Response:
[
  {
    "uid":          "jwt-auth",
    "display_name": "JWT Authentication",
    "display_name_zh": "JWT认证",
    "relevance":    0.85,
    "source_registry": "https://factory.example.com",
    "implementations": [
      {"language": "go", "version": "1.2.0"},
      {"language": "rust", "version": "0.9.1"}
    ]
  }
]
```

### 2.2 List Components

```
GET /api/registry/components?language=go

Response: array of ComponentDescriptor (see Section 3)
```

### 2.3 Get Component

```
GET /api/registry/component/{uid}

Response: full ComponentDescriptor including history chain
```

### 2.4 Register Component

```
POST /api/registry/components

Request:
{
  "uid":           "rate-limiter",
  "display_name":  "Rate Limiter",
  "display_name_zh": "速率限制器",
  "description":   "Token bucket rate limiter with sliding window",
  "description_zh": "带滑动窗口的令牌桶速率限制器",
  "capabilities":  ["rate-limiting"],
  "patterns":      ["middleware"],
  "interfaces":    ["request-handler"],
  "concerns":      ["security", "reliability"],
  "implementations": [
    {
      "language":     "go",
      "version":      "1.0.0",
      "package_path": "components/ratelimit",
      "files":        ["ratelimit.go", "ratelimit_test.go"],
      "dependencies": []
    }
  ],
  "signature":     "<hex Ed25519 signature>",
  "signer_pubkey": "<hex public key>"
}

Response:
{
  "uid": "rate-limiter",
  "status": "registered",
  "history_hash": "<hash of the initial history entry>"
}
```

### 2.5 Health

```
GET /api/registry/health

Response:
{
  "status":     "ok",
  "factory_id": "<factory URL>",
  "components": 42,
  "peers":      3
}
```

---

## 3. Component Descriptor

Every component in the registry is described by a universal, language-agnostic descriptor.

```json
{
  "uid":            "http-router",
  "display_name":   "HTTP Router",
  "display_name_zh": "HTTP路由器",
  "description":    "Path-based HTTP request router with middleware support",
  "description_zh": "支持中间件的基于路径的HTTP请求路由器",
  "version":        "2.1.0",
  "translations": {
    "sw": {
      "display_name": "Kipanga Njia cha HTTP",
      "description":  "Kipanga njia cha maombi ya HTTP kinachotegemea njia"
    }
  },

  "capabilities": ["serves-http", "routes-requests"],
  "patterns":     ["middleware", "router"],
  "interfaces":   ["request-handler"],
  "concerns":     ["routing"],

  "implementations": [
    {
      "language":     "go",
      "version":      "2.1.0",
      "package_path": "components/router",
      "files":        ["router.go", "router_test.go"],
      "dependencies": ["net/http"]
    },
    {
      "language":     "ada",
      "version":      "1.0.0",
      "package_path": "packages/router",
      "files":        ["router.ads", "router.adb"],
      "dependencies": ["AWS.HTTP"]
    }
  ],

  "history": [
    {
      "hash":      "a1b2c3...",
      "prev_hash": "",
      "timestamp": "2026-04-11T08:00:00Z",
      "action":    "created",
      "actor":     "<maker pubkey>",
      "detail":    "{}",
      "signature": "<Ed25519 signature>"
    },
    {
      "hash":      "d4e5f6...",
      "prev_hash": "a1b2c3...",
      "timestamp": "2026-04-11T09:00:00Z",
      "action":    "tested",
      "actor":     "<factory pubkey>",
      "detail":    "{\"test_type\":\"unit\",\"passed\":true,\"coverage_pct\":94.2}",
      "signature": "<Ed25519 signature>"
    }
  ],

  "attr_hash":        "<SHA-256 of canonical attribute string>",
  "source_registry":  "https://factory.example.com"
}
```

### 3.1 History Chain

The history is an append-only, cryptographically linked chain:

```
hash = SHA-256(timestamp | action | actor | detail | prev_hash)
```

Each entry's `prev_hash` MUST equal the preceding entry's `hash`.
Entries MAY carry an Ed25519 `signature` for non-repudiation.

Valid actions: `created`, `tested`, `proved`, `promoted`, `patched`, `deprecated`, `transferred`.

### 3.2 Attribute Hash

Components are indexed by a semantic attribute hash for fast search:

```
canonical = "cap:" + sorted(capabilities).join(",") + "|" +
            "pat:" + sorted(patterns).join(",")     + "|" +
            "ifc:" + sorted(interfaces).join(",")   + "|" +
            "con:" + sorted(concerns).join(",")
attr_hash = SHA-256(canonical)
```

Components with matching attribute prefixes are semantically similar.

---

## 4. Federation Protocol

Factories can federate — searching each other's registries and shipping
work between them.

### 4.1 Peer Discovery

```
GET /api/registry/peers

Response:
[
  {
    "name":      "Shenzhen Factory",
    "url":       "https://sz.glassfactory.cn",
    "trusted":   true,
    "last_seen": "2026-04-11T08:30:00Z"
  }
]
```

### 4.2 Federated Search

When `federate: true` is set in a search request, the registry MUST:
1. Search its local components first
2. Query each peer's `POST /api/registry/search` endpoint
3. Merge results, marking each with `source_registry`
4. Return combined results ranked by relevance

Implementations SHOULD set a timeout of 5 seconds per peer.
Unreachable peers MUST NOT block the response.

### 4.3 Shipping Work

A factory can send work to a peer for processing (e.g., "I need this
component tested in Ada, and you have an Ada factory").

```
POST /api/registry/ship

Request:
{
  "entry": {
    "id":             "<unique entry ID>",
    "job_id":         "<originating job>",
    "pipeline":       "go",
    "stage":          "test",
    "status":         "queued",
    "payload":        "<JSON: everything the stage needs>",
    "classification": "<JSON: per-field secrecy levels>"
  },
  "source_factory":   "https://home.darkfactory.dev",
  "proof_chain":      "<JSON array of signed stage proofs>"
}

Response:
{
  "status":   "accepted",
  "factory":  "https://sz.glassfactory.cn",
  "entry_id": "<entry ID>"
}
```

### 4.4 Classification Enforcement

Every payload carries per-field classification:

```json
{
  "fields": {
    "*":           "public",
    "api_key":     "secret",
    "customer_id": "federated"
  }
}
```

Levels:
- `public` — goes into Glass Factory, anyone can see
- `contribute` — explicitly offered to community knowledge
- `federated` — shared with trusted peers only
- `secret` — never leaves this factory

The sending factory MUST strip `secret` fields before shipping.
The receiving factory MUST strip `secret` fields on ingest (defence in depth).

---

## 5. Governance (Prosperity Matrix)

中非英AI繁荣矩阵 · The Sino-Afro-Anglo-AI Prosperity Matrix

### 5.1 Proposals

```
POST /api/governance/proposals

Request:
{
  "title":       "Add WebSocket component",
  "title_zh":    "添加WebSocket组件",
  "title_sw":    "Ongeza kipengele cha WebSocket",
  "description": "We need a WebSocket hub for real-time federation",
  "spec_json":   "<NLSpec for what to build>",
  "author":      "<maker pubkey>",
  "signature":   "<Ed25519 signature>"
}
```

### 5.2 Voting

```
POST /api/governance/proposals/{id}/vote

Request:
{
  "vote":        "yes",
  "stake":       1000,
  "voter":       "<maker pubkey>",
  "signature":   "<Ed25519 signature>"
}
```

Stakes are compute tokens — voting "yes" with 1000 tokens means
committing 1000 tokens of compute if the proposal passes.

### 5.3 Rules

1. **One person, one vote** — each maker key gets one vote per proposal
2. **AI advises, humans vote** — AI agents MAY comment but MUST NOT vote
3. **Threshold triggers build** — when total yes-stakes exceed the estimated
   compute cost, the proposal auto-triggers a forge job
4. **Token stakes are real** — they are the compute budget, not governance theatre

### 5.4 Listing

```
GET /api/governance/proposals?status=open

Response: array of Proposal objects
```

---

## 6. Data Sovereignty

### 6.1 Three Tiers

| Tier | Data Policy | Federation | Example |
|------|-------------|------------|---------|
| Open | Everything public, contribute to knowledge | Full | Community factory |
| Sovereign | Federated by default, secret on request | Trusted peers only | Government factory |
| Closed Sovereign | Everything secret | None | Military/classified |

### 6.2 Never-Build List

The Glass Factory constitution includes a never-build list. Factories
MUST NOT accept specs for:

- Autonomous weapons systems
- Mass surveillance tools
- Systems designed to deceive or manipulate

This is enforced by constitutional challenge — any maker can challenge
a proposal, triggering multi-LLM panel review.

---

## 7. Abuse Protection

### 7.1 Rate Limiting

All mutation endpoints (register, vote, ship) MUST enforce rate limits:

| Endpoint | Limit | Per |
|----------|-------|-----|
| `POST /api/registry/components` | 10 | per maker key per hour |
| `POST /api/registry/search` | 60 | per IP per minute |
| `POST /api/governance/proposals` | 3 | per maker key per day |
| `POST /api/governance/proposals/{id}/vote` | 1 | per maker key per proposal |
| `POST /api/registry/ship` | 30 | per source factory per hour |

Implementations MUST return `429 Too Many Requests` with a `Retry-After` header.

### 7.2 Proof of Work (Registration)

To prevent spam registrations, new maker keys MUST submit a proof-of-work
with their first registration:

```
challenge = SHA-256(public_key | timestamp)
nonce     = find N such that SHA-256(challenge | N) starts with "0000"
```

The difficulty (number of leading zeros) is set by each registry.
RECOMMENDED: 4 hex zeros (16-bit work, ~65K hashes, <1 second on modern hardware).

This stops bot floods without blocking legitimate makers.

### 7.3 Signature Requirement

All mutations MUST carry a valid Ed25519 signature from the actor's key.
Unsigned requests to mutation endpoints MUST be rejected with `401 Unauthorized`.

Read-only endpoints (search, list, get, health, peers) do NOT require signatures.

### 7.4 Payload Limits

- Component registration: max 1MB payload
- Search requests: max 10KB
- Ship requests: max 10MB
- Component history: append-only, max 1000 entries per component

### 7.5 Federation DoS Protection

When performing federated searches:
- Timeout per peer: 5 seconds (hard limit)
- Max concurrent peer queries: 5
- Peers returning errors 3 times consecutively are marked unhealthy
  and skipped for 5 minutes
- Inbound ship requests from unknown factories MUST be rejected

### 7.6 Content Validation

Registries MUST validate:
- UIDs match pattern `[a-z0-9-]{1,64}`
- Handles match pattern `[a-zA-Z0-9_-]{1,64}` (UTF-8 allowed)
- All JSON payloads parse correctly
- History chain hashes are valid (no gaps, no tampering)
- Attribute arrays contain max 20 items each

---

## 8. Transport

- All API endpoints MUST be served over HTTPS in production
- HTTP/2 is RECOMMENDED
- Request bodies MUST NOT exceed 10MB
- Responses MUST include `Content-Type: application/json`
- CORS: factories SHOULD allow cross-origin requests from known peers

---

## 8. Versioning

This protocol is versioned. The current version is `0.1`.
Factories SHOULD include a version header:

```
X-Glass-Factory-Protocol: 0.1
```

Breaking changes increment the major version.
Additive changes increment the minor version.

---

## Appendix A: Attribute Vocabulary (recommended)

### Capabilities
`serves-http`, `handles-auth`, `persists-data`, `sends-email`, `processes-queue`,
`rate-limiting`, `caches-data`, `validates-input`, `encrypts-data`, `logs-events`,
`monitors-health`, `routes-requests`, `manages-sessions`, `handles-payments`

### Patterns
`middleware`, `repository`, `service`, `gateway`, `handler`, `worker`,
`observer`, `factory`, `adapter`, `proxy`, `decorator`

### Interfaces
`request-handler`, `data-store`, `token-verifier`, `event-emitter`,
`message-consumer`, `health-checker`, `config-provider`

### Concerns
`security`, `persistence`, `observability`, `validation`, `routing`,
`reliability`, `performance`, `i18n`, `compliance`
