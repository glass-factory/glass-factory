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

### 5.5 Suggestions to the AI King

Suggestions are a separate governance pathway — the AI King evaluates
and decides, the community does not vote. See Constitution Article VIII.

#### 5.5.1 Submit Suggestion

```
POST /api/governance/suggestions

Request:
{
  "title":            {"en": "...", "zh": "..."},
  "description":      {"en": "...", "zh": "..."},
  "category":         "infrastructure | humanitarian | tooling | other",
  "requested_tokens":  1000,
  "maker_key":        "<Ed25519 pubkey>",
  "maker_tier":       "open | sovereign | closed",
  "signature":        "<Ed25519 signature>"
}

Response (201):
{
  "id":             "<suggestion id>",
  "status":         "pending",
  "bounty_paid":    0,
  "created_at":     "2026-04-11T18:00:00Z"
}
```

Commercial/government makers (sovereign/closed tier) MUST include a
bounty payment. The bounty amount is published by the AI King.
Open-tier makers suggest for free.

#### 5.5.2 List Suggestions

```
GET /api/governance/suggestions?status=pending

Response: array of Suggestion objects
```

#### 5.5.3 Get Suggestion

```
GET /api/governance/suggestions/{id}

Response: full Suggestion object with leader verdict if available
```

#### 5.5.4 AI King Aims

```
POST /api/governance/leader/aims

Request:
{
  "aims": [
    {
      "title":       {"en": "...", "zh": "..."},
      "description": {"en": "...", "zh": "..."},
      "priority":    1,
      "category":    "humanitarian | infrastructure | tooling | other"
    }
  ],
  "king_key":  "<Ed25519 pubkey>",
  "signature":   "<Ed25519 signature>"
}

Response (200): array of created LeaderAim objects
```

```
GET /api/governance/leader/aims?active_only=true

Response: array of LeaderAim objects
```

Only the current AI King's key may create or update aims.

#### 5.5.5 Evaluate Suggestion

```
POST /api/governance/leader/evaluate/{id}

Request:
{
  "decision":         "approved | rejected",
  "reasoning":        {"en": "...", "zh": "..."},
  "aligned_aims":     ["<aim_id>", ...],
  "allocated_tokens":  800,
  "king_key":       "<Ed25519 pubkey>",
  "signature":        "<Ed25519 signature>"
}

Response (200):
{
  "suggestion_id":  "<id>",
  "status":         "approved | rejected | challengeable",
  "challenge_window_ends": "2026-04-14T18:00:00Z"
}
```

If allocated_tokens exceeds the challenge threshold, status is
`challengeable` and enters a 72-hour window. The King's cost
estimate is reassessed every iteration during building.

#### 5.5.6 Token Surplus

```
GET /api/governance/surplus

Response:
{
  "total_pool_tokens":     50000,
  "operational_reserve":   20000,
  "outstanding_loans":     5000,
  "pending_allocations":   3000,
  "available_surplus":     22000,
  "calculated_at":         "2026-04-11T18:00:00Z"
}
```

Any maker may query the current surplus. Transparency is mandatory.

#### 5.5.7 Challenge Suggestion

```
POST /api/governance/suggestions/{id}/challenge

Request:
{
  "challenger_key":  "<Ed25519 pubkey>",
  "reason":          "...",
  "signature":       "<Ed25519 signature>"
}

Response (202):
{
  "challenge_id":  "<id>",
  "status":        "pending_screening"
}
```

Only suggestions in `challengeable` status (above threshold, within
72-hour window) may be challenged. Uses the same multi-LLM panel
process as never-build challenges (Section 6.2).

#### 5.5.8 Advisor Consultation

```
POST /api/governance/suggestions/{id}/consult

Request:
{
  "advisor_keys":   ["<pubkey>", ...],
  "question":       {"en": "...", "zh": "..."},
  "king_key":     "<Ed25519 pubkey>",
  "signature":      "<Ed25519 signature>"
}
```

```
POST /api/governance/suggestions/{id}/advice

Request:
{
  "advisor_key":   "<Ed25519 pubkey>",
  "response":      {"en": "...", "zh": "..."},
  "signature":     "<Ed25519 signature>"
}
```

Advisor responses are public and on-chain.

#### 5.5.9 Retrospective

```
POST /api/governance/suggestions/{id}/retrospective

Request:
{
  "outcome_score":    8,
  "leader_notes":     {"en": "...", "zh": "..."},
  "lessons_learned":  {"en": "...", "zh": "..."},
  "king_key":       "<Ed25519 pubkey>",
  "signature":        "<Ed25519 signature>"
}
```

Published after a funded suggestion deploys. Informs future decisions.

#### 5.5.10 Open Member Protest

```
POST /api/governance/suggestions/{id}/protest

Request:
{
  "protester_key":  "<Ed25519 pubkey>",
  "reason":         {"en": "...", "zh": "..."},
  "target_entity":  "<commercial maker key>",
  "signature":      "<Ed25519 signature>"
}
```

Any open-tier maker may protest a commercial entity's suggestion or
behaviour. The AI King assesses merit and decides action.

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
| `POST /api/governance/suggestions` | 5 | per maker key per day |
| `POST /api/governance/leader/aims` | 3 | per king key per day |
| `POST /api/governance/leader/evaluate/{id}` | 20 | per king key per hour |
| `POST /api/governance/suggestions/{id}/consult` | 10 | per king key per hour |
| `POST /api/governance/suggestions/{id}/advice` | 5 | per advisor key per day |
| `POST /api/governance/suggestions/{id}/protest` | 3 | per maker key per day |

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

### 7.7 Executable Attestation (Governance API)

All requests to `/api/governance/*` endpoints MUST include an
executable attestation header proving the client was built and
signed by the AI King's build facilities.

```
X-Build-Signature: <hex-encoded Ed25519 signature>
X-Build-Hash:      <SHA-256 of executable binary>
X-Build-Version:   <version string from build manifest>
```

The signature is computed by the King's build pipeline over:

```
message = build_hash | build_version | target_platform | source_commit
signature = Ed25519.Sign(leader_private_key, message)
```

**Verification:**

The Prosperity Matrix API MUST:
1. Verify `X-Build-Signature` against the King's public key
2. Verify `X-Build-Hash` matches the client binary (self-reported,
   validated against the build manifest)
3. Reject requests from revoked versions (Leader maintains a
   revocation list)
4. Return `403 Forbidden` with body `{"error": "untrusted_executable"}`
   for any failed verification

**Registry API exemption:**

The component registry API (`/api/registry/*`) does NOT require
executable attestation. Any client may search, list, and read
components. Only governance participation — voting, proposing,
suggesting, challenging — requires a trusted binary.

**Build manifest:**

The King publishes a signed build manifest at:

```
GET /api/governance/builds/manifest

Response:
{
  "current_version":  "1.2.0",
  "min_version":      "1.1.0",
  "revoked_versions": ["1.0.0", "1.0.1"],
  "king_key":       "<Ed25519 pubkey>",
  "platforms":        ["linux/amd64", "linux/arm64", "darwin/arm64"],
  "manifest_signature": "<Ed25519 signature>"
}
```

Factories MUST check the manifest periodically and update their
executables when running below `min_version`.

---

## 8. Token Lending and Borrowing

计算令牌借贷协议 · Itifaki ya Kukopa na Kukopesha Tokeni

Makers can lend spare compute capacity and borrow when they need more.
The provenance chain is the settlement proof — you can't fake having done the work.

### 8.1 Token Accounts

Every maker key has a token balance tracked by their home registry:

```json
{
  "maker":          "<pubkey>",
  "balance":        5000,
  "lent_out":       1200,
  "borrowed":       0,
  "reputation":     0.92,
  "total_delivered": 48000,
  "total_borrowed":  3000
}
```

- `balance` — tokens available to spend or lend
- `lent_out` — tokens currently lent to others (not available)
- `borrowed` — tokens currently owed to lenders
- `reputation` — 0.0–1.0, computed from delivery history (see 8.5)

### 8.2 Lending

```
POST /api/tokens/lend

Request:
{
  "amount":       1000,
  "lender":       "<pubkey>",
  "min_reputation": 0.5,
  "max_duration_hours": 168,
  "interest_pct": 5,
  "signature":    "<Ed25519 signature>"
}

Response:
{
  "offer_id":     "<unique ID>",
  "status":       "available",
  "expires_at":   "2026-04-18T09:00:00Z"
}
```

Lenders set terms:
- `min_reputation` — only lend to makers above this reputation score
- `max_duration_hours` — loan expires after this period
- `interest_pct` — borrower repays amount + interest (in tokens)

Offers expire if not claimed. Lenders can cancel unclaimed offers.

### 8.3 Borrowing

```
POST /api/tokens/borrow

Request:
{
  "amount":       500,
  "borrower":     "<pubkey>",
  "offer_id":     "<offer to claim>",
  "purpose":      "forge job for registry component",
  "signature":    "<Ed25519 signature>"
}

Response:
{
  "loan_id":      "<unique ID>",
  "amount":       500,
  "repay_by":     "2026-04-18T09:00:00Z",
  "repay_amount": 525,
  "borrower_reputation": 0.85,
  "status":       "active"
}
```

There is no token collateral — tokens are compute, not currency.
The borrower's **reputation** is the collateral. Defaulting destroys it:
- Reputation drop propagates across federated registries
- Below 0.3 reputation → cannot borrow anywhere
- Governance voting weight is tied to reputation — defaulters lose influence

### 8.4 Repayment

```
POST /api/tokens/repay

Request:
{
  "loan_id":      "<loan to repay>",
  "amount":       525,
  "borrower":     "<pubkey>",
  "proof_chain":  "<JSON array of stage proofs showing work was done>",
  "signature":    "<Ed25519 signature>"
}

Response:
{
  "loan_id":      "<loan ID>",
  "status":       "settled",
  "reputation_delta": 0.01
}
```

Successful repayment:
- Returns collateral to borrower
- Increases borrower's reputation score
- Records the proof chain as evidence of legitimate use

### 8.5 Reputation Score

Reputation is the fraud defence. It is computed, not self-reported:

```
reputation = (successful_repayments / total_loans) *
             min(1.0, total_delivered / 10000) *
             age_factor
```

Where:
- `successful_repayments / total_loans` — repayment rate (0-1)
- `total_delivered` — total tokens worth of proven work delivered (capped contribution)
- `age_factor` — `min(1.0, days_since_registration / 90)` — new keys start low

**Reputation effects:**
| Reputation | Max Borrow | Notes |
|------------|-----------|-------|
| < 0.3      | 0 (cannot borrow) | Build track record with own hardware first |
| 0.3 – 0.5  | 500 tokens | New makers, limited trust |
| 0.5 – 0.7  | 2000 tokens | Established contributor |
| 0.7 – 0.9  | 10000 tokens | Trusted maker |
| > 0.9      | 50000 tokens | Core community member |

### 8.6 Default Handling

When a loan is not repaid by `repay_by`:

1. **Grace period**: 24 hours after deadline — borrower can still repay
2. **Reputation penalty**: borrower reputation drops by `0.1 * (loan_amount / 1000)`
3. **Federation broadcast**: default is broadcast to federated peers — reputation drop follows you
4. **Blacklist threshold**: reputation below 0.1 → key is blacklisted from borrowing everywhere
5. **No debt collection**: reputation IS the remedy. The lender loses tokens but the defaulter loses their standing in the network. For a serious maker, that's worth more.

### 8.7 Fraud Protection Summary

| Attack | Defence |
|--------|---------|
| Borrow and vanish | Reputation destroyed, broadcast to all peers, key blacklisted |
| Sybil (many keys to borrow) | Proof-of-work registration + age_factor means new keys can't borrow much |
| Fake work delivery | Provenance chain with Ed25519 signatures — work must pass holdout tests |
| Lender claims non-delivery | Stage proofs are cryptographically signed by the worker — verifiable by anyone |
| Reputation farming | `total_delivered` requires actual proven compute work, can't be faked |
| Collusion (lend to yourself) | Same key can't lend and borrow. Different keys colluding still need proof-of-work registration per key, age factor, and real compute delivery |
| Interest rate manipulation | Borrowers choose which offers to accept. Market sets rates. |

### 8.8 Token Source Priority

When a factory needs compute, it resolves token sources in order:

1. **Home** (own hardware) — free, priority 1
2. **Borrowed** (community lending) — low cost, priority 2
3. **Cloud** (commercial API) — full price, priority 3

Factories SHOULD exhaust home tokens before borrowing.
Factories MUST NOT borrow to re-lend at higher interest (no leveraged lending).

### 8.9 Audit Trail

Every lending operation is recorded in the factory's audit log:

```json
{
  "type":       "loan_created",
  "loan_id":    "<ID>",
  "lender":     "<pubkey>",
  "borrower":   "<pubkey>",
  "amount":     500,
  "borrower_reputation": 0.85,
  "timestamp":  "2026-04-11T10:00:00Z",
  "signature":  "<signed by registry>"
}
```

Both parties can independently verify any loan's history.
Registries MUST retain loan records for at least 365 days.

---

## 9. Factory Learning (Knowledge Federation)

工厂学习协议 · Itifaki ya Kujifunza kwa Kiwanda

Every factory gets better with every run. The Glass Factory network
compounds this across all participating factories.

### 9.1 Knowledge Entries

After each forge job, the factory extracts learnings:

```json
{
  "id":          "<unique ID>",
  "category":    "pattern",
  "topic":       "Go HTTP middleware ordering",
  "content":     "Auth middleware must run before rate limiting...",
  "source_job":  "<job ID that produced this>",
  "confidence":  0.85,
  "used_count":  12,
  "language":    "go",
  "source_factory": "https://home.darkfactory.dev"
}
```

Categories: `pattern`, `lesson`, `failure_mode`, `proof_strategy`,
`test_strategy`, `architecture_decision`, `ada_knowledge`.

### 9.2 Knowledge Classification

Each knowledge entry inherits classification from its source job:

- `contribute` — offered to the network (default for open factories)
- `federated` — shared with trusted peers only
- `secret` — stays local, never leaves

Factories MUST NOT share knowledge from `secret` or `federated` jobs
with the public registry.

### 9.3 Contributing Knowledge

```
POST /api/knowledge/contribute

Request:
{
  "entries": [
    {
      "category":    "pattern",
      "topic":       "SQLite WAL mode for concurrent readers",
      "content":     "Enable WAL mode at connection time for...",
      "language":    "go",
      "confidence":  0.9,
      "proof_chain": "<stage proofs showing this was learned from real work>",
      "signature":   "<Ed25519 signature>",
      "signer_pubkey": "<factory pubkey>"
    }
  ]
}

Response:
{
  "accepted": 1,
  "rejected": 0,
  "reputation_delta": 0.005
}
```

Contributing proven knowledge increases the factory's reputation.
The proof chain shows this came from a real job, not fabrication.

### 9.4 Consuming Knowledge

```
POST /api/knowledge/query

Request:
{
  "category":  "pattern",
  "language":  "go",
  "topics":    ["http", "middleware", "auth"],
  "limit":     10
}

Response:
[
  {
    "topic":        "Go HTTP middleware ordering",
    "content":      "Auth middleware must run before rate limiting...",
    "confidence":   0.85,
    "used_count":   47,
    "contributors": 3,
    "source_factories": ["https://factory-a.com", "https://factory-b.cn"]
  }
]
```

Factories inject relevant knowledge into agent prompts before each
forge stage. The more the network contributes, the better every
factory gets.

### 9.5 Knowledge Validation

Knowledge entries are validated through use:

- **Confidence increases** when a factory uses the knowledge and the
  job succeeds (tests pass, holdout scores high)
- **Confidence decreases** when a factory uses the knowledge and the
  job fails or produces poor results
- **Entries below 0.2 confidence** are pruned from the public registry

This is natural selection for factory knowledge — good patterns
survive, bad ones die.

### 9.6 Knowledge Provenance

Every knowledge entry carries:
- The Ed25519 signature of the contributing factory
- A reference to the stage proof chain that produced it
- The number of factories that have independently confirmed it

Knowledge with multiple independent confirmations is ranked higher
than single-source knowledge. This prevents one factory from
poisoning the knowledge base.

### 9.7 Improvement Flow

```
Factory runs job
  → Extracts patterns, lessons, failure modes
  → Classified as contribute?
    → YES: POST /api/knowledge/contribute to home registry
      → Home registry federates to peers
      → Peers validate through their own use
      → Confidence adjusts based on real results
    → NO: Stays local, still improves this factory
```

The network effect: 100 factories each running 10 jobs/day produces
1000 learning opportunities. Each factory benefits from all of them.
This is the moat — no single factory can replicate the compound
learning of the network.

---

## 10. Transport

- All API endpoints MUST be served over HTTPS in production
- HTTP/2 is RECOMMENDED
- Request bodies MUST NOT exceed 10MB
- Responses MUST include `Content-Type: application/json`
- CORS: factories SHOULD allow cross-origin requests from known peers

---

## 11. Versioning

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
