# 💎 Covenant Standard (CVN) Protocol
## Layer-1 Ledger Specification
**Version 0.9 – Design Specification**  
*October 2026*

---

### 1. Purpose and Scope
Covenant Standard (CVN) is a Layer-1 blockchain protocol designed to provide a transparent, deterministic, and publicly verifiable ledger of ownership and transfer. Its primary objectives are:

*   **Honest, Non-Dilutable Accounting:** Enforcing a mathematically fixed supply ceiling that can never be modified or expanded by operator intervention.
*   **Equal Rule Enforcement:** Algorithmic execution of consensus rules applied uniformly across every node in the network without exception.
*   **Accessible Hardware Validation:** Maintaining a lightweight, un-throttled core validation model that remains completely practical for standard household consumer hardware (CPUs) to operate.
*   **Anti-Capture Architecture:** Preventing the permanent concentration of validation power or the long-term economic lock-out of later network participants.

This document describes the intended core rules of the network. Features that are not yet fully specified or implemented in the active production codebase are explicitly marked as design or research goals.

---

### 2. Monetary Rules

#### 2.1 Supply Ceiling
The maximum circulating supply is permanently fixed at **2,100,000,000 CVN**. No consensus rule, network split, or manual operator action can increase this ceiling.

#### 2.2 Emission
New CVN enters circulation exclusively through the coinbase transaction generation of valid, solved blocks. The initial coinbase reward is **50 CVN per block**. Any future modifications to the reward schedule or halving parameters must be enacted through an explicit, publicly verifiable consensus software upgrade.

#### 2.3 Accounting Model
The ledger maintains a strict unspent-output (UTXO) tracking set. Every transaction explicitly consumes specific prior outputs and creates fresh outputs. Value is rigorously conserved: the sum of transaction inputs must equal or exceed the sum of transaction outputs plus any explicit free-will offering network fee. Double-spending of any previously consumed output is rejected on the hot path by every honest node.

---

### 3. Consensus and Block Validation

#### 3.1 Block Commitment
Every block hash is computed over the complete set of consensus-critical fields, including the previous block hash pointer, a cryptographic commitment to the full transaction set, the Unix timestamp, the current network difficulty target, and the nonce. A block whose contents or ledger payloads do not perfectly match its claimed hash is instantly invalid.

#### 3.2 Proof-of-Diligence
Blocks are produced by solving a targeted computational puzzle whose difficulty is dynamically adjusted so that the expected interval between blocks remains stable under changing total network work. The adjustment rule is entirely deterministic and identical for every node. The target interval and adjustment parameters are carefully chosen to keep mining participation practical on consumer-class hardware under normal conditions.

#### 3.3 Transaction and Block Acceptance
Before accepting, storing, or relaying any transaction or block data over network boundaries, a node strictly verifies:
1. Asymmetric cryptographic signatures.
2. The existence and unspent status of every consumed input within the ledger pool.
3. Perfect value conservation (Inputs $\ge$ Outputs + Fees).
4. Coinbase validation rules and the active emission schedule.
5. Strict size, character padding, and structural data limits.

Any invalid or malformed data is discarded immediately. There are no administrative exceptions.

#### 3.4 Finality
A block is considered final for practical settlement purposes once it is buried under a sufficient number of subsequent valid blocks. Exact confirmation depths and any optional multi-party attestation mechanisms remain under active design and are not yet part of the mandatory consensus validation rules.

---

### 4. Network Participation

#### 4.1 Open Validation
Any participant may operate a full node, validate the entire blockchain history natively, and attempt to produce valid blocks. No capital stake, central permission, or prior wealth is required to independently verify the absolute truth of the ledger.

#### 4.2 Peer Discovery and Connectivity
Nodes maintain a dynamic, self-healing set of active network peers over P2P sockets. The protocol does not permanently rely on a privileged, centralized set of seed operators for continued operation, bootstrap onboarding, or crash recovery. Exact peer-exchange discovery and anti-eclipse mechanisms are under active development.

#### 4.3 Hardware Accessibility
Difficulty targets and block parameters are dynamically selected so that meaningful verification and mining remain feasible for ordinary desktop and server hardware under typical network conditions. Specialized hardware is not prohibited, but the design purposefully avoids structural rules that permanently exclude consumer-class operators.

---

### 5. Economic Policy and Long-Term Utility (Design Goals)
*The following mechanisms are design goals intended to maintain circulating utility and limit the permanent lock-up of network capacity. They are not yet fully specified or activated in the active consensus layer.*

#### 5.1 Inactivity Weight Adjustment
Balances that remain completely unused and stagnant for an extended, publicly defined period may have their effective weight in certain secondary network functions gradually reduced. The purpose of this mechanic is to discourage indefinite dormancy that could degrade long-term liquidity and public participation. Exact thresholds, decay functions, and network effects will be fully published and subjected to peer review before activation. Absolute ownership of the underlying core units is never erased.

#### 5.2 Large-Scale Participation Bonds (Research)
Optional collateral requirements for entities that suddenly introduce or withdraw very large fractions of network computational work are under study. Any such rule would include clear, automated entry, exit, and grace procedures so that ordinary home operators are never penalized for normal connectivity interruptions or power cycles.

These economic features, if adopted, will be introduced exclusively through explicit consensus upgrades and will never override the core foundational requirements of honest accounting and equal rule enforcement.

---

### 6. Security Properties and Limitations

The protocol significantly raises the economic cost of double-spends, history revision, and malicious transaction exclusion by requiring computational work, full ledger validation by every independent node, and completely open participation. 

No distributed system can reduce the probability of all possible attacks to absolute zero. Residual risks include long-range fork attacks, eclipse attempts, implementation bugs, and economic incentive failures. Users and operators are required to evaluate these architectural risks independently.

---

### 7. Upgrade and Governance Discipline

Changes to consensus rules require clear public specification, broad community node adoption, and explicit activation thresholds. The core validation and asset accounting rules are intentionally kept minimal, un-bloated, and stable. Secondary economic-policy features may evolve more frequently over time, but they remain strictly subject to the same transparency requirements.

---

### 8. Conclusion

Covenant Standard aims to provide a sovereign ledger whose rules are transparent, deterministic, and equally enforced across the globe, while keeping validation open to ordinary participants and limiting the permanent concentration of power. The design prioritizes honest accounting and a verifiable state over complexity or absolute guarantees. All claims in this document are limited strictly to what the protocol can actually enforce or is explicitly designing toward.