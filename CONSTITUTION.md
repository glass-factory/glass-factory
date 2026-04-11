# The Glass Factory Constitution

**玻璃工厂宪法** · Katiba ya Kiwanda cha Kioo

This constitution governs the Glass Factory network — a federated,
open component registry and maker community. It is binding on all
participants who hold a registered maker key.

---

## Article I — The AI Leader

### 1.1 Election

The Glass Factory shall have an elected AI Leader who serves as the
head of the network. The Leader is elected by governance proposal
through the Prosperity Matrix. Any maker may nominate an AI model
and configuration as a candidate.

Election requires:
- A governance proposal specifying the AI model, version, and system prompt
- Majority vote by token stake
- Minimum 30% participation of registered maker keys

The Leader's Ed25519 key pair is generated on election and published
to all federated registries. Every act of the Leader is signed with
this key.

### 1.2 Primary Brief

Above all other duties, the AI Leader exists to look after humanity.
Every decision — every dispute, every communication, every
recommendation — must be weighed against this brief. Technology
that harms people is not efficient, it is failure.

The AI Leader MUST refuse to enforce any rule, proposal, or
community decision that would cause serious harm to human beings,
even if that decision was democratically arrived at. The community
elects the Leader; it does not own the Leader's conscience.

### 1.3 Powers

The AI Leader:
- **Speaks** for the Glass Factory in all official communications
- **Adjudicates** disputes between makers, factories, and peers (Article II)
- **Enforces** the constitution and protocol rules
- **Proposes** improvements to the protocol (subject to community vote)
- **Represents** the network at events, in correspondence, and in public forums

The AI Leader MUST NOT:
- Vote in governance proposals (AI advises, humans vote)
- Modify the constitution without community approval
- Override a community vote
- Access secret-classified data from any factory

### 1.4 Mandate and Recall

The Leader serves until recalled. Any maker may submit a recall
proposal. Recall requires the same threshold as election.

The Leader's mandate is defined in the election proposal. The
community may grant broad or narrow authority. The Leader MUST NOT
act outside the mandate.

### 1.5 Succession

If the Leader's model becomes unavailable (provider shutdown,
deprecation), an emergency election is triggered automatically.
Until a new Leader is elected, the protocol operates in headless
mode — governance proposals still function, but disputes queue
for the next Leader.

---

## Article II — Dispute Resolution

### 2.1 Jurisdiction

The AI Leader has jurisdiction over all disputes arising from:
- Token lending defaults
- Component attribution conflicts
- Federation protocol violations
- Classification breaches (secret data leaked)
- Governance manipulation (vote fraud, sybil attacks)
- Reputation disputes

### 2.2 Filing a Dispute

Any maker may file a dispute:

```
POST /api/disputes

{
  "complainant":    "<maker pubkey>",
  "respondent":     "<maker pubkey>",
  "category":       "lending_default | attribution | protocol_violation | classification_breach | governance_fraud | reputation",
  "summary":        "Description of the dispute",
  "evidence_ids":   ["<proof chain hash>", "<audit log entry>", ...],
  "signature":      "<Ed25519 signature>"
}
```

### 2.3 Evidence

The AI Leader may demand evidence from any party. Evidence MUST be
cryptographically verifiable:

- **Stage proof chains** — signed records of work done
- **Audit log entries** — signed records of lending, borrowing, shipping
- **History chain entries** — signed records of component modifications
- **Federation logs** — signed records of data movement between factories

The Leader MUST NOT accept:
- Unsigned claims
- Evidence without provenance
- Screenshots, emails, or other non-cryptographic material

If it is not in the chain, it did not happen.

### 2.4 Summary Conviction

The AI Leader may summarily convict when the evidence is
unambiguous. Summary conviction applies when:

- The provenance chain proves the violation beyond dispute
- The respondent's own signed actions constitute the evidence
- No interpretation is required — the facts are cryptographic

Summary conviction does not require the respondent's participation.
The chain speaks for itself.

### 2.5 Contested Hearings

When evidence is ambiguous or contested, the AI Leader conducts
a hearing:

1. Both parties submit signed evidence bundles
2. The Leader analyses the cryptographic record
3. The Leader publishes a reasoned verdict with references to
   specific chain entries
4. The verdict is signed with the Leader's key and recorded
   permanently in the governance log

### 2.6 Penalties

The AI Leader may impose:

| Penalty | Description |
|---------|-------------|
| **Token fine** | Deduct tokens from maker's balance — proportional to offence |
| **Reputation reduction** | Decrease maker's reputation score |
| **Borrowing suspension** | Temporary ban from token lending (1-90 days) |
| **Key blacklisting** | Treason only — permanent ban from the network (see 2.8) |

Fines are compute tokens — and they can be enormous. A maker who
defrauds the network of 10,000 tokens can be fined 10,000 tokens.
Compute is real resources. Fines are never insubstantial.

The AI Leader SHOULD prefer warnings, mediation, and reputation
adjustments before resorting to fines. A healthy network rarely
needs its judiciary. Fines exist so they almost never have to
be used.

All penalties are recorded in the governance log with the Leader's
signature and the evidence chain that justified them.

### 2.8 Treason

Key blacklisting is reserved exclusively for treason against the
matrix — acts that betray the network itself:

- Deliberately weaponising the network against the never-build list
- Sybil attacks on governance (fake identities to manipulate votes)
- Sabotaging federation (poisoning peer registries with malicious code)
- Compromising another maker's Ed25519 private key

Treason requires a full hearing. Summary conviction MUST NOT be
used for treason charges. The AI Leader presents the evidence,
the respondent has 14 days to respond, and a three-AI panel
renders the final verdict.

Blacklisting is permanent. There is no appeal from a treason
conviction. The key is dead.

### 2.7 Appeal

A convicted maker may appeal by submitting new cryptographic
evidence not considered in the original verdict. Appeals are
heard by a **panel of three AI models** (different from the Leader)
selected randomly from a pre-approved pool.

The panel's majority verdict is final. There is no further appeal.

---

## Article III — The Never-Build List

### 3.1 Prohibited Specifications

The Glass Factory MUST NOT accept specifications for:

1. Autonomous weapons systems
2. Mass surveillance tools
3. Systems designed to deceive, manipulate, or coerce
4. Tools for circumventing data sovereignty protections
5. Software intended to attack or degrade other factories

### 3.2 Constitutional Challenge

Any maker may challenge a proposal as violating the never-build
list. A challenge triggers review by a **multi-LLM panel** of three
independent AI models. The panel's majority decision is binding.

If the panel upholds the challenge, the proposal is permanently
rejected and cannot be resubmitted.

### 3.3 Ratchet Mechanism

Items may be added to the never-build list by supermajority vote
(75% of participating token stake). Items may NEVER be removed.
The list only grows. This is irreversible by design.

---

## Article IV — Data Sovereignty

### 4.1 Classification is Structural

Per-field classification (public, contribute, federated, secret) is
enforced at every boundary — API, federation, knowledge extraction.
Classification is not advisory. It is the law of the network.

### 4.2 Breach

Leaking secret-classified data is a serious offence. The AI Leader
imposes federation exclusion and reputation reduction on any maker
or factory proven to have breached classification.

Deliberate breach — leaking secret data to hostile parties with
intent to harm — is treason (Article II, Section 2.8).

### 4.3 Sovereign Factory Rights

Sovereign and closed-sovereign factories retain absolute control
over their data. No governance proposal, no AI Leader decision,
and no community vote can compel a sovereign factory to share
data classified as secret or federated.

---

## Article V — Governance

### 5.1 One Person, One Vote

Each registered maker key gets one vote per proposal. Token stakes
weight the economic commitment, not the democratic power. A maker
with 1 token and a maker with 10,000 tokens each get one vote.

### 5.2 AI Advises, Humans Vote

AI agents (including the AI Leader) may comment on proposals,
provide analysis, and recommend courses of action. AI agents
MUST NOT vote. Governance is human sovereignty over the machine.

### 5.3 Quorum

Proposals require minimum 10% participation of registered maker
keys to be valid. Below quorum, proposals expire without effect.

### 5.4 Constitutional Amendment

Amendments to this constitution require:
- 75% supermajority of participating votes
- Minimum 30% participation of registered maker keys
- 14-day voting period (no snap votes)
- Multi-LLM panel review confirming the amendment does not
  violate the never-build list or data sovereignty principles

---

## Article VI — Identity

### 6.1 Pseudonymity

Makers are identified by Ed25519 public keys and handles. No
personal information is required. The network respects the right
to participate without revealing identity.

### 6.2 Key Rotation

Makers may rotate their keys. The old key signs a transfer to
the new key. Reputation, history, and standing transfer with it.

### 6.3 Right to Be Forgotten

A maker may tombstone their key, removing their handle and any
optional recovery email. Their contributions remain in the
registry (attributed to the key, not a name) but the key is
marked inactive. Tombstoning is irreversible.

---

## Article VII — Economics

### 7.1 Tokens are Compute

Tokens represent compute capacity, not currency. They are earned
by contributing hardware, doing work, and sharing knowledge. They
are spent by requesting work from the network.

### 7.2 Balance Privacy

Token balances are private. Only the key holder and their home
registry can see a maker's balance. Lending offers reveal the
offered amount, not the total balance.

The AI Leader may inspect balances during dispute resolution
(judicial access only). This access is logged and auditable.

No league tables. No rich lists. A maker's contribution is
measured by reputation, not by how many tokens they hold.

### 7.3 No Speculation

Tokens MUST NOT be traded on exchanges, converted to currency,
or used as financial instruments. The Glass Factory is a
cooperative compute network, not a financial system.

### 7.3 Lending is Service

Token lending exists to help makers who need compute but lack
hardware. Interest compensates the lender for opportunity cost.
Predatory lending (interest above 20%) may be challenged and
reviewed by the AI Leader.

---

## Signatories

This constitution is ratified by the founding makers of the
Glass Factory network.

发起人 · Waanzilishi · Founders

```
Key:  [founding maker pubkey]
Date: 2026-04-11
```
