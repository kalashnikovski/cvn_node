<script>
	import { onMount } from 'svelte';
	import { GetLocalNodeTelemetry, FetchWalletBalanceQuery, BroadcastLocalTransactionSubmit } from '../wailsjs/wailsjs/go/main/App.js';

	let localHeight = $state(0);
	let globalTip = $state(0); 
	let blockHash = $state("FETCHING_MAINNET_EPOCH_SIGNATURE_MATRIX...");
	let activeWallet = $state("SECURE_IDENTITY_PROFILE_UNTETHERED");
	let walletBalance = $state(0.00);

	let txRecipient = $state("");
	let txAmount = $state("");
	let txFee = $state("0.01");
	let txStatusMessage = $state("");

	let syncPercentage = $derived(
		globalTip > 0 ? Math.min(100, Math.floor((localHeight / globalTip) * 100)) : 0
	);
	let isSynced = $derived(globalTip > 0 && localHeight >= globalTip);

	async function updateNodeTelemetry() {
		try {
			const telemetryDataString = await GetLocalNodeTelemetry();
			if (telemetryDataString) {
				const nodeData = JSON.parse(telemetryDataString);
				
				localHeight = nodeData.current_height;
				blockHash = nodeData.block_hash;
				activeWallet = nodeData.miner_address;
				
				if (nodeData.current_height > globalTip) {
					globalTip = nodeData.current_height;
				}
			}

			if (activeWallet && activeWallet !== "SECURE_IDENTITY_PROFILE_UNTETHERED") {
				const currentBalance = await FetchWalletBalanceQuery(activeWallet);
				walletBalance = currentBalance;
			}
		} catch (error) {
			console.error("Matrix telemetry interface link offline:", error);
		}
	}

	onMount(() => {
		updateNodeTelemetry();
		const loopInterval = setInterval(updateNodeTelemetry, 1500);
		return () => clearInterval(loopInterval);
	});

	async function handleSendTransaction(e) {
		e.preventDefault();
		if (!txRecipient || !txAmount) {
			txStatusMessage = "❌ ERROR: Recipient address and transfer tokens are required.";
			return;
		}
		
		txStatusMessage = "⏳ SIGNING CRYPTOGRAPHIC TRANSIT BLOCKS NATIVELY...";

		try {
			const resultMessage = await BroadcastLocalTransactionSubmit(txRecipient, txAmount, txFee);
			
			if (resultMessage.includes("SUCCESS")) {
				txStatusMessage = `✅ BROADCAST COMPLETE: Injected ${txAmount} CVN transfer payload into mainnet channels!`;
				txRecipient = "";
				txAmount = "";
				updateNodeTelemetry();
			} else {
				txStatusMessage = `🚨 REJECTED: ${resultMessage}`;
			}
		} catch (error) {
			txStatusMessage = `❌ INTERNAL EXCEPTION: Failed to commit payload to P2P network lanes.`;
		}
	}

	function copyAddressToClipboard() {
		navigator.clipboard.writeText(activeWallet);
		txStatusMessage = "📋 Core identity wallet address copied straight to clipboard tracking channels!";
	}
</script>

<main style="background: #06060a; color: #ffffff; min-height: 100vh; font-family: monospace; padding: 2rem;">
	<div style="max-width: 900px; margin: 0 auto; display: flex; flex-direction: column; gap: 1.5rem;">
		
		<!-- Sync Progress Visualization Panel -->
		<div style="border: 1px solid {isSynced ? '#00ff66' : '#00ffff'}; padding: 1rem 1.5rem; border-radius: 4px; background: #0e0e18; box-shadow: 0 0 10px rgba(0,255,255,0.05);">
			<div style="display: flex; justify-content: space-between; margin-bottom: 0.5rem; font-size: 0.9rem;">
				<span style="color: #8a8a9e;">NETWORK LIFELINE BLOCK SYNCHRONIZATION PROGRESS:</span>
				<span style="color: {isSynced ? '#00ff66' : '#00ffff'}; font-weight: bold;">
					{isSynced ? "✨ 100% SYNCHRONIZED" : `📡 SYNCING LAYER CHANNELS: ${syncPercentage}%`}
				</span>
			</div>
			
			<div style="width: 100%; background: #161624; height: 10px; border-radius: 5px; overflow: hidden; border: 1px solid #222;">
				<div style="width: {syncPercentage || 100}%; background: linear-gradient(90deg, #00ffff, #00ff66); height: 100%; transition: width 0.3s ease-out;"></div>
			</div>
			
			<div style="display: flex; justify-content: space-between; font-size: 0.75rem; margin-top: 0.4rem; color: #8a8a9e;">
				<span>Local Height: #{localHeight}</span>
				<span>Target Mainnet Tip: #{globalTip || localHeight}</span>
			</div>
		</div>

		<!-- UI Grid System Layer -->
		<div style="display: grid; grid-template-columns: 1fr 1fr; gap: 2rem;">
			
			<!-- Left Side Status Matrix -->
			<div style="border: 1px solid #00ff66; padding: 1.5rem; border-radius: 4px; background: #0e0e18; box-shadow: 0 0 15px rgba(0,255,102,0.1);">
				<h2 style="color: #00ff66; margin-top: 0;">💎 CVN MATRIX CORE</h2>
				<p style="color: #8a8a9e; font-size: 0.85rem; margin-bottom: 1.5rem;">LAYER-1 PROTOCOL CONSENSUS ENGINE VALIDATOR</p>
				
				<div style="display: flex; flex-direction: column; gap: 1rem; border-top: 1px solid #222; padding-top: 1.5rem;">
					<div>
						<span style="color: #00ffff;">[MESH MATRIX STANDING]</span> 
						<span style="color: {isSynced ? '#00ff66' : '#00ffff'}; font-weight: bold; background: rgba(0,255,102,0.05); padding: 0.2rem 0.5rem; border-radius: 3px;">
							{isSynced ? "CORE_RIG // FULL_NODE" : "OPERATIONAL // MINING_ACTIVE"}
						</span>
					</div>
					<div>
						<span style="color: #00ffff;">[AVAILABLE COIN RESERVE]</span> 
						<span style="font-weight: bold; font-size: 1.3rem; color: #00ff66;">
							<!-- 👑 THE DYNAMIC COMPONENT REPAIR: Robust fallback ensures 0 syntax exceptions during early loading phases -->
							{(Number(walletBalance) || 0).toFixed(2)} CVN
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

			<!-- Right Side Transfer Core -->
			<div style="border: 1px solid #00ffff; padding: 1.5rem; border-radius: 4px; background: #0e0e18; box-shadow: 0 0 15px rgba(0,255,255,0.1);">
				<h2 style="color: #00ffff; margin-top: 0;">📦 BROADCAST PAYLOAD</h2>
				<p style="color: #8a8a9e; font-size: 0.85rem; margin-bottom: 1.5rem;">TRANSMIT CRYPTOGRAPHIC CASH VALUE OVER MAINNET WIRE</p>
				
				<form onsubmit={handleSendTransaction} style="display: flex; flex-direction: column; gap: 1rem; border-top: 1px solid #222; padding-top: 1.5rem;">
					<div style="display: flex; flex-direction: column; gap: 0.3rem;">
						<label for="txRecipientInput" style="color: #8a8a9e; font-size: 0.8rem;">RECIPIENT DESTINATION ADDRESS</label>
						<input id="txRecipientInput" type="text" bind:value={txRecipient} placeholder="Enter target cvn_address..." style="background: #06060a; border: 1px solid #333; color: #fff; padding: 0.6rem; font-family: monospace; border-radius: 3px;" />
					</div>

					<div style="display: grid; grid-template-columns: 1fr 1fr; gap: 1rem;">
						<div style="display: flex; flex-direction: column; gap: 0.3rem;">
							<label for="txAmountInput" style="color: #8a8a9e; font-size: 0.8rem;">TRANSFER AMOUNT</label>
							<input id="txAmountInput" type="number" step="any" bind:value={txAmount} placeholder="0.00" style="background: #06060a; border: 1px solid #333; color: #00ff66; font-weight: bold; padding: 0.6rem; font-family: monospace; border-radius: 3px;" />
						</div>
						<div style="display: flex; flex-direction: column; gap: 0.3rem;">
							<label for="txFeeInput" style="color: #8a8a9e; font-size: 0.8rem;">VOLUNTARY PRIORITY FEE</label>
							<input id="txFeeInput" type="text" bind:value={txFee} style="background: #06060a; border: 1px solid #333; color: #00ffff; padding: 0.6rem; font-family: monospace; border-radius: 3px;" />
						</div>
					</div>

					<button type="submit" style="background: #00ffff; color: #000; border: none; padding: 0.8rem; font-weight: bold; font-family: monospace; cursor: pointer; border-radius: 3px; margin-top: 0.5rem; text-transform: uppercase; letter-spacing: 1px;">
						🚀 Sign and Broadcast Transaction
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