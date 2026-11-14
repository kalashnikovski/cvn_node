package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"cvn_node/internal/randomx"
)

// RandomX speed test, started with `--randomx-bench`. It measures how many
// RandomX hashes per second this machine computes with the engine the miner
// uses, without starting a node or touching any chain. Options:
//
//	--threads N    mining threads to use (default: every processor thread)
//	--seconds N    how long to measure (default 30)
//
// Set CVN_RANDOMX_LIGHT=1 to measure the low-memory mode instead.

func benchArgument(name string, fallback int) int {
	for i, arg := range os.Args {
		if arg == name && i+1 < len(os.Args) {
			if value, err := strconv.Atoi(os.Args[i+1]); err == nil && value > 0 {
				return value
			}
		}
	}
	return fallback
}

// RunRandomXBenchmark runs the speed test and prints the result.
func RunRandomXBenchmark() {
	threads := benchArgument("--threads", runtime.NumCPU())
	seconds := benchArgument("--seconds", 30)

	fmt.Println("====================================================================")
	fmt.Println("⚙️  CVN RANDOMX SPEED TEST")
	fmt.Println("====================================================================")
	fmt.Printf(" Threads: %d   Duration: %d s\n", threads, seconds)

	flags := randomx.GetFlags()
	cache, err := randomx.NewCache(flags)
	if err != nil {
		fmt.Println("🚨 Could not prepare RandomX:", err)
		return
	}
	defer cache.Close()
	started := time.Now()
	cache.Init([]byte("CVN speed test key"))

	mode := "low-memory"
	var dataset *randomx.Dataset
	if os.Getenv("CVN_RANDOMX_LIGHT") == "" {
		if created, fullFlags, largePages, err := newMiningDataset(flags); err == nil {
			created.InitDatasetParallel(cache, threads)
			dataset, flags, mode = created, fullFlags, "full-memory"
			if largePages {
				mode = "full-memory, large pages"
			}
			defer dataset.Close()
		} else {
			fmt.Println(" Not enough memory for the full dataset; measuring low-memory mode.")
		}
	}
	fmt.Printf(" Mode: %s   Set-up time: %v\n", mode, time.Since(started).Round(100*time.Millisecond))
	if os.Getenv("CVN_NO_PINNING") == "" {
		fmt.Println(" Threads pinned to processor threads: yes")
	}
	fmt.Println(" Measuring...")

	var total atomic.Int64
	var wait sync.WaitGroup
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	measureStart := time.Now()
	for t := 0; t < threads; t++ {
		wait.Add(1)
		go func(thread int) {
			defer wait.Done()
			pinMiningThread(thread)
			var vm *randomx.VM
			var err error
			if dataset != nil {
				vm, err = randomx.NewVM(flags, nil, dataset)
			} else {
				vm, err = randomx.NewVM(flags, cache, nil)
			}
			if err != nil {
				return
			}
			defer vm.Close()
			// 32 bytes, like the block identifier the miner hashes.
			input := make([]byte, 32)
			binary.LittleEndian.PutUint32(input[4:], uint32(thread))
			var output [randomx.RANDOMX_HASH_SIZE]byte
			for count := uint32(0); time.Now().Before(deadline); count++ {
				binary.LittleEndian.PutUint32(input, count)
				vm.CalculateHash(input, &output)
				total.Add(1)
			}
		}(t)
	}
	wait.Wait()
	elapsed := time.Since(measureStart).Seconds()
	rate := float64(total.Load()) / elapsed

	fmt.Println("--------------------------------------------------------------------")
	fmt.Printf(" Result: %.0f hashes per second in total\n", rate)
	fmt.Printf("         %.0f hashes per second per thread\n", rate/float64(threads))
	fmt.Println("====================================================================")
}
