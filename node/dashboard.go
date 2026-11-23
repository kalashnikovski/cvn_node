package node

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"sync/atomic"
	"time"
)

// THE MINER WINDOW
//
// Every four seconds the node works out how fast it is mining and, when a
// person is watching, redraws the window: the height, the balance of the
// mining address, the speed over the last minute, quarter hour, hour and
// day, whether it is mining or why not, and the last few lines of what has
// happened. Run as a service, with nobody watching, it writes one line every
// few minutes instead.

const (
	dashboardInterval = 4 * time.Second
	// How many readings, one each interval, make up each average.
	readingsIn15Minutes = 225
	readingsInAnHour    = 900
	readingsInADay      = 21600
	readingsInAMinute   = 15
)

// miningSpeeds are the averages shown in the window, in millions of hashes a
// second, as the rest of the program counts speed.
type miningSpeeds struct {
	now, lastMinute, last15Minutes, lastHour, lastDay float64
}

// runDashboard keeps the window up to date. It does not return.
func runDashboard(seedOnly bool) {
	onScreen := stdoutIsTerminal()
	ticker := time.NewTicker(dashboardInterval)
	defer ticker.Stop()
	if runtime.GOOS == "windows" {
		_ = exec.Command("cmd", "/c", "cls").Run()
	}
	for tick := uint64(1); ; tick++ {
		<-ticker.C
		StateMutex.RLock()
		height := GetLatestBlockInternal().Index
		StateMutex.RUnlock()

		BalanceCacheMutex.RLock()
		balance := UnitsAmount(StateBalanceCache[CustomMinerAddress]).CVN()
		BalanceCacheMutex.RUnlock()
		speeds := recordMiningSpeed()
		if tick%10 == 0 {
			noteMiningActivity(seedOnly, speeds.now, height)
		}
		// The window is for a person at a terminal. Run as a service, with
		// output going to a log, redrawing it every few seconds would only
		// fill the log; one line every few minutes says the node is alive.
		if !onScreen {
			logMutex.Lock()
			if len(liveTerminalEventLogs) > 12 {
				liveTerminalEventLogs = liveTerminalEventLogs[len(liveTerminalEventLogs)-12:]
			}
			logMutex.Unlock()
			if tick%75 == 1 {
				log.Printf("💓 Running. Height #%d.\n", height)
			}
			continue
		}
		drawDashboard(seedOnly, height, balance, speeds)
	}
}

// recordMiningSpeed takes the speed over the interval just ended, adds it to
// the readings kept, and returns the averages.
func recordMiningSpeed() miningSpeeds {
	hashes := atomic.SwapUint64(&globalHashCount, 0)
	now := float64(hashes) / dashboardInterval.Seconds() / 1000000.0
	// While the fast miner does the work, show the speed it reports.
	if reported, ok := fastMinerHashrate(); ok {
		now = reported / 1000000.0
	}
	// A pool shows the speed of its miners together, read from their shares.
	if poolRunning() {
		poolSpeed, _ := poolSpeedNow()
		now = poolSpeed / 1000000.0
	}
	currentLiveHashrateTrack = now

	hashrateMutex.Lock()
	defer hashrateMutex.Unlock()
	hashRateHistory15m = appendKeeping(hashRateHistory15m, now, readingsIn15Minutes)
	hashRateHistory1h = appendKeeping(hashRateHistory1h, now, readingsInAnHour)
	hashRateHistory24h = appendKeeping(hashRateHistory24h, now, readingsInADay)
	// The longer averages cover only the time since the miner started until
	// their window has filled.
	return miningSpeeds{
		now:           now,
		lastMinute:    averageOfLast(hashRateHistory15m, readingsInAMinute),
		last15Minutes: averageOfLast(hashRateHistory15m, len(hashRateHistory15m)),
		lastHour:      averageOfLast(hashRateHistory1h, len(hashRateHistory1h)),
		lastDay:       averageOfLast(hashRateHistory24h, len(hashRateHistory24h)),
	}
}

// appendKeeping adds a reading and drops the oldest once more than `keep`
// are held.
func appendKeeping(readings []float64, reading float64, keep int) []float64 {
	readings = append(readings, reading)
	if len(readings) > keep {
		readings = readings[1:]
	}
	return readings
}

// noteMiningActivity adds a line to the window's log saying what the node is
// doing: mining and how fast, not mining and why, or following as a seed.
func noteMiningActivity(seedOnly bool, speed float64, height int64) {
	at := time.Now().Format("15:04:05")
	var line string
	if seedOnly {
		line = fmt.Sprintf("[%s] 🌱 [SEED] Not mining. Following the network // Ledger Height: #%d", at, height)
	} else if poolRunning() {
		poolSpeed, miners := poolSpeedNow()
		line = fmt.Sprintf("[%s] ⛏️  [POOL] %d miner(s) at %s // height #%d", at, miners, formatHashrate(poolSpeed/1000000.0), height)
	} else if status, working := minerStatus(height); !working {
		// Not mining for now. Say why, and not "Mining at 0 H/s".
		line = fmt.Sprintf("[%s] ⏳ [SYNC] Not mining yet. %s // height #%d", at, status, height)
	} else {
		line = fmt.Sprintf("[%s] ⛏️  [MINING] Mining at %s // height #%d", at, formatHashrate(speed), height)
	}
	logMutex.Lock()
	liveTerminalEventLogs = append(liveTerminalEventLogs, line)
	logMutex.Unlock()
}

// drawDashboard clears the terminal and draws the window.
func drawDashboard(seedOnly bool, height int64, balance float64, speeds miningSpeeds) {
	logMutex.Lock()
	defer logMutex.Unlock()
	clear := exec.Command("clear")
	if runtime.GOOS == "windows" {
		clear = exec.Command("cmd", "/c", "cls")
	}
	clear.Stdout = os.Stdout
	_ = clear.Run()

	// The heading and the figures are green, the line above the log purple;
	// in the log only the tags are coloured (terminal_colors.go).
	const rule = "================================================================================"
	fmt.Println(inGreen(rule))
	fmt.Println(inGreen("💎 COVENANT STANDARD (CVN) MINER                                                "))
	fmt.Println(inGreen(rule))
	fmt.Println(inGreen(fmt.Sprintf("🛰️  NETWORK HEIGHT: #%-15d | 📊 60s AVG HASHRATE: %s", height, formatHashrate(speeds.lastMinute))))
	fmt.Println(inGreen(fmt.Sprintf("💰 WALLET BALANCE: %-16.2f CVN | 📊 15m AVG HASHRATE: %s", balance, formatHashrate(speeds.last15Minutes))))
	if seedOnly {
		fmt.Println(inGreen(fmt.Sprintf("🌱 SEED NODE: NOT MINING            | 📊 60m AVG HASHRATE: %s", formatHashrate(speeds.lastHour))))
	} else if poolRunning() {
		_, miners := poolSpeedNow()
		fmt.Println(inGreen(fmt.Sprintf("⛏️  POOL: %-4d MINER(S) CONNECTED     | 📊 60m AVG HASHRATE: %s", miners, formatHashrate(speeds.lastHour))))
	} else {
		fmt.Println(inGreen(fmt.Sprintf("🪪 MINING ADDRESS: %-16.16s...  | 📊 60m AVG HASHRATE: %s", CustomMinerAddress, formatHashrate(speeds.lastHour))))
	}
	status, working := minerStatus(height)
	fmt.Println(inGreen(fmt.Sprintf("📶 STATUS: %-24s | 📊 24h AVG HASHRATE: %s", minerStatusShort(height), formatHashrate(speeds.lastDay))))
	fmt.Println(inGreen(rule))
	fmt.Println(inPurple("⚡ RECENT ACTIVITY:                                                             "))
	fmt.Println(inPurple("--------------------------------------------------------------------------------"))

	// Only as many log lines as fit under the heading are kept and shown,
	// newest last, so the heading never scrolls out of view however fast
	// lines arrive.
	keep := dashboardLogLines(terminalRows())
	if !working {
		// Catching up or waiting: the whole of it, with its progress bar, on
		// a line of its own above the log.
		fmt.Println(inGreen("⏳ " + status))
		keep--
	}
	if len(liveTerminalEventLogs) > keep {
		liveTerminalEventLogs = liveTerminalEventLogs[len(liveTerminalEventLogs)-keep:]
	}
	if len(liveTerminalEventLogs) == 0 {
		fmt.Println(colourLogTags(fmt.Sprintf("[%s] ⛏️  [MINING] Mining at %s // height #%d", time.Now().Format("15:04:05"), formatHashrate(speeds.now), height)))
	}
	for _, event := range liveTerminalEventLogs {
		fmt.Println(colourLogTags(event))
	}
	os.Stdout.Sync()
}
