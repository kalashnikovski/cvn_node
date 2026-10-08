<script>
	import { onMount } from 'svelte';

	// Svelte 5 Reactive State Runes (Completely Cleaned)
	let localHeight = $state(26922);
	let globalTip = $state(27460); 
	let blockHash = $state("85632def04401fccf7cbccd78b9ceb4d64c87a9195996921209dd653726b5ebd");
	let activeWallet = $state("CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337");
	let walletBalance = $state(510921.50);

	// Form Inputs State Mappings
	let txRecipient = $state("");
	let txAmount = $state("");
	let txFee = $state("0.01");
	let txStatusMessage = $state("");

	// Dynamic Sync Calculation Property Runes
	let syncPercentage = $derived(
		globalTip > 0 ? Math.min(100, Math.floor((localHeight / globalTip) * 100)) : 0
	);
	let isSynced = $derived(localHeight >= globalTip);

	onMount(() => {
		const syncInterval = setInterval(() => {
			if (localHeight < globalTip) {
				localHeight += 12; 
				walletBalance += 12345.50; 
			} else {
				localHeight = globalTip;
				walletBalance = 1024580.75; 
				clearInterval(syncInterval);
			}
		}, 100);

		return () => clearInterval(syncInterval);
	});

	async function handleSendTransaction(e) {
		e.preventDefault();
		if (!txRecipient || !txAmount) {
			txStatusMessage = "❌ ERROR: Recipient address and transfer tokens are required.";
			return;
		}
		if (!isSynced) {
			txStatusMessage = "❌ TRANSIT BLOCKED: Cannot broadcast transactions while node is synchronizing.";
			return;
		}
		txStatusMessage = "⏳ COMPILING CRYPTOGRAPHIC TRANSIT BLOCKS...";
		setTimeout(() => {
			txStatusMessage = `✅ SUCCESS: Injected ${txAmount} CVN transfer payload into mainnet mempool!`;
			txRecipient = "";
			txAmount = "";
		}, 1200);
	}

	function copyAddressToClipboard() {
		navigator.clipboard.writeText(activeWallet);
		alert("📋 Core identity wallet address copied straight to clipboard tracking channels!");
	}
</script>

<main style="background: #06060a; color: #ffffff; min-height: 100vh; font-family: monospace; padding: 2rem;">
	<div style="max-width: 900px; margin: 0 auto; display: flex; flex-direction: column; gap: 1.5rem;">
		
		<!-- 🛰️ Live Network Sync Status Bar Panel -->
		<div style="border: 1px solid {isSynced ? '#00ff66' : '#00ffff'}; padding: 1rem 1.5rem; border-radius: 4px; background: #0e0e18; box-shadow: 0 0 10px rgba(0,255,255,0.05);">
			<div style="display: flex; justify-content: space-between; margin-bottom: 0.5rem; font-size: 0.9rem;">
				<span style="color: #8a8a9e;">NETWORK LIFELINE BLOCK SYNCHRONIZATION PROGRESS:</span>
				<span style="color: {isSynced ? '#00ff66' : '#00ffff'}; font-weight: bold;">
					{isSynced ? "✨ 100% SYNCHRONIZED" : `📡 SYNCING: ${syncPercentage}%`}
				</span>
			</div>
			
			<div style="width: 100%; background: #161624; height: 10px; border-radius: 5px; overflow: hidden; border: 1px solid #222;">
				<div style="width: {syncPercentage}%; background: linear-gradient(90deg, #00ffff, #00ff66); height: 100%; transition: width 0.1s linear;"></div>
			</div>
			
			<div style="display: flex; justify-content: space-between; font-size: 0.75rem; margin-top: 0.4rem; color: #8a8a9e;">
				<span>Local Height: #{localHeight}</span>
				<span>Target Mainnet Tip: #{globalTip}</span>
			</div>
		</div>

		<!-- CORE INTERACTIVE PANEL ROW -->
		<div style="display: grid; grid-template-columns: 1fr 1fr; gap: 2rem;">
			
			<!-- LEFT SIDE: Live Core Node Telemetry Status Cards -->
			<div style="border: 1px solid #00ff66; padding: 1.5rem; border-radius: 4px; background: #0e0e18; box-shadow: 0 0 15px rgba(0,255,102,0.1);">
				<h2 style="color: #00ff66; margin-top: 0;">💎 CVN MATRIX CORE</h2>
				<p style="color: #8a8a9e; font-size: 0.85rem; margin-bottom: 1.5rem;">LAYER-1 PROTOCOL CONSENSUS ENGINE VALIDATOR</p>
				
				<div style="display: flex; flex-direction: column; gap: 1rem; border-top: 1px solid #222; padding-top: 1.5rem;">
					<div>
						<span style="color: #00ffff;">[MESH MATRIX STANDING]</span> 
						<span style="color: {isSynced ? '#00ff66' : '#00ffff'}; font-weight: bold; background: rgba(0,255,102,0.05); padding: 0.2rem 0.5rem; border-radius: 3px;">
							{isSynced ? "CORE_RIG // FULL_NODE" : "BOOTSTRAPPING // LEDGER_SYNC"}
						</span>
					</div>
					<div>
						<span style="color: #00ffff;">[AVAILABLE COIN RESERVE]</span> 
						<span style="font-weight: bold; font-size: 1.3rem; color: #00ff66;">
							{walletBalance.toLocaleString(undefined, {minimumFractionDigits: 2, maximumFractionDigits: 2})} CVN
						</span>
					</div>
					<div>
						<span style="color: #00ffff;">[PUBLIC IDENTITY ADDRESS]</span>
						<div style="display: flex; gap: 0.5rem; margin-top: 0.3rem;">
							<input type="text" readonly value={activeWallet} style="background: #06060a; border: 1px solid #333; color: #e6c229; padding: 0.5rem; font-family: monospace; font-size: 0.75rem; flex: 1; border-radius: 3px;" />
							<button onclick={copyAddressToClipboard} style="background: #222; border: 1px solid #444; color: #fff; padding: 0.5rem; cursor: pointer; border-radius: 3px;">📋</button>
						</div>
					</div>
					<div style="word-break: break-all; font-size: 0.8rem; color: #8a8a9e; line-height: 1.3;">
						<span style="color: #00ffff;">[CHAIN TIP SIGNATURE]</span> <br/>{blockHash}
					</div>
				</div>
			</div>

			<!-- RIGHT SIDE: Interactive Cash Transfer Value Matrix Form -->
			<div style="border: 1px solid #00ffff; padding: 1.5rem; border-radius: 4px; background: #0e0e18; box-shadow: 0 0 15px rgba(0,255,255,0.1);">
				<h2 style="color: #00ffff; margin-top: 0;">📦 BROADCAST PAYLOAD</h2>
				<p style="color: #8a8a9e; font-size: 0.85rem; margin-bottom: 1.5rem;">TRANSMIT CRYPTOGRAPHIC CASH VALUE OVER MAINNET WIRE</p>
				
				<form onsubmit={handleSendTransaction} style="display: flex; flex-direction: column; gap: 1rem; border-top: 1px solid #222; padding-top: 1.5rem;">
					<div style="display: flex; flex-direction: column; gap: 0.3rem;">
						<label style="color: #8a8a9e; font-size: 0.8rem;">RECIPIENT DESTINATION ADDRESS</label>
						<input type="text" bind:value={txRecipient} placeholder="Enter target cvn_address..." style="background: #06060a; border: 1px solid #333; color: #fff; padding: 0.6rem; font-family: monospace; border-radius: 3px;" />
					</div>

					<div style="display: grid; grid-template-columns: 1fr 1fr; gap: 1rem;">
						<div style="display: flex; flex-direction: column; gap: 0.3rem;">
							<label style="color: #8a8a9e; font-size: 0.8rem;">TRANSFER AMOUNT</label>
							<input type="number" step="any" bind:value={txAmount} placeholder="0.00" style="background: #06060a; border: 1px solid #333; color: #00ff66; font-weight: bold; padding: 0.6rem; font-family: monospace; border-radius: 3px;" />
						</div>
						<div style="display: flex; flex-direction: column; gap: 0.3rem;">
							<label style="color: #8a8a9e; font-size: 0.8rem;">VOLUNTARY PRIORITY FEE</label>
							<input type="text" bind:value={txFee} style="background: #06060a; border: 1px solid #333; color: #00ffff; padding: 0.6rem; font-family: monospace; border-radius: 3px;" />
						</div>
					</div>

					<button type="submit" disabled={!isSynced} style="background: {isSynced ? '#00ffff' : '#222'}; color: {isSynced ? '#000' : '#555'}; border: none; padding: 0.8rem; font-weight: bold; font-family: monospace; cursor: {isSynced ? 'pointer' : 'not-allowed'}; border-radius: 3px; margin-top: 0.5rem; text-transform: uppercase; letter-spacing: 1px;">
						{isSynced ? "🚀 Sign and Broadcast Transaction" : "🔒 Syncing Ledger Channels..."}
					</button>
					
					{#if txStatusMessage}
						<div style="background: #06060a; padding: 0.8rem; border-radius: 3px; font-size: 0.85rem; text-align: center; border: 1px dashed #333; color: #00ffff; margin-top: 0.5rem;">
							{txStatusMessage}
						</div>
					{/if}
				</form>
			</div>

		</div>
	</div>
</main>
