# 🛰️ COVENANT STANDARD (CVN) — PHASE 3 TECHNICAL BLUEPRINTS
### Compiled Post-Launch Architecture & Sandbox Diagnostic Pass
**Date Check:** Wednesday, 12:59 AM | Melbourne, Australia

---

## 🔬 1. THE SPARKS60 INSIGHTS (USER-EXPERIENCE TELEMETRY)
During the initial 48-hour live mainnet deployment, alpha testing from user `u/Sparks60` exposed two critical client-side bottlenecks that define our Phase 3 development roadmap:

*   **The Zip Age Catch-Up Trap:** Providing a static, historical ledger bootstrap file (`cvn_mainnet.zip`) caused the user's client to load an outdated block checkpoint (#13,765). Because the mainnet tip was advancing past #16,800, the client immediately initialized local PoD mining loops on an orphaned branch instead of waiting to catch up over the wire.
*   **The Visual Balance Cache Disconnect:** Because the background utility task `go RebuildStateBalanceCache()` requires time to parse BoltDB blocks sequentially on boot, the web interface temporarily displayed a fallback placeholder of `0.00 CVN`. This caused severe user panic, as the terminal output displayed local block wins that were mathematically destined to be overwritten and discarded by the true chain's heavier difficulty work weights.
*   **The "LOCAL" Print Confusion:** The standard text string output `💰 LOCAL NODE REWARD AUDIT` misled the user into believing their client was executing inside an isolated, offline testing sandbox rather than a globally connected peer socket framework.

---

## 🛠️ 2. PHASE 3 SYSTEM SPECIFICATIONS & CORE PATCHES

### FEATURE A: The Sync-Gate Engine Lock & Progress Bar HUD
To completely eliminate user confusion and stop clients from wasting electricity mining orphaned blocks, implement a strict initialization lock before launching the Proof-of-Diligence (PoD) loops.
*   **The Logic:** Freeze all local block-generation worker loops on boot if the local database height is lower than the peer mesh tip height.
*   **The HUD Interface:** Render a non-scrolling, clean terminal progress percentage matrix:
    `⏳ [MAINNET LEDGER SYNC] Progress: [==========>    ] 74.2% (Catching up to Tip)`
*   **The Release:** Automatically unleash the local CPU mining threads *only* after confirming the local node has achieved 100% mathematical alignment with the global mainnet tip.

### FEATURE B: Automated Credential `.gitignore` Safeguards
To protect incoming open-source contributors from accidentally leaking private hash keys onto public GitHub repositories during casual repository updates:
*   **The Patch:** Integrate a standardized `.gitignore` template file directly into the repository root.
*   **The Target Rules:** Forcefully restrict Git from ever staging or pushing `my_crypto_address.txt` or `miner_config.json` configuration variables live to the cloud.

### FEATURE C: End-to-End Encrypted P2P Mesh Messaging
Leverage the existing Horizontal Gossip Mesh TCP architecture to route secure, private peer-to-peer messages directly over active network socket tracks without bloating the main chain database.
*   **The Payload Schema:** Declare a lightweight metadata payload frame containing `SenderID`, `RecipientID`, `EncryptedPayload`, and a cryptographic private key signature.
*   **The Security Layer:** Apply ECIES (Elliptic Curve Integrated Encryption Scheme) using the users' existing asymmetric public wallet address keys to lock the plain text. 
*   **The Cost Model:** Keep messaging free and dynamic by storing the message frames strictly inside local temporary RAM logs (Off-Chain Matrix style). Messages drop away from the active network tracks the moment they are read, maintaining a lightweight, streamlined database.

---

## 🌎 3. REALLOCATED GLOBAL RING TOPOLOGY
To satisfy hosting provider limits while maintaining complete mainnet security, our multi-anchor network layout has evolved:

1.  **Melbourne (Master Core):** Handles 100% of primary PoD block validation using un-throttled AMD Ryzen 9 7950X3D CPU hardware.
2.  **Singapore (APAC Gateway):** Throttled via the `--no-mining` and `--idle-threads` hotfix flags to operate as a low-overhead network routing seed and dashboard explorer, dropping vCPU footprint to 1%.
3.  **North America (Western Anchor):** Future deployment utilizing a Dedicated Virtual Server (VDS) with guaranteed hardware allocations to serve our 33.2% US viewer demographic without CPU throttling constraints.
4.  **Greece (Mediterranean Crossroads):** A multi-terabit subsea landing hub node to route blocks instantly into Europe and Africa with minimal latency.
