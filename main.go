package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// The command to start Anvil
const anvilCommand string = "/root/.foundry/bin/anvil"

// The Anvil ipc path
const ipcPath string = "/tmp/anvil.ipc"

// The message at which Anvil starts up
const startupMessage string = "Listening on"

// File containing the Anvil state
const anvilState string = "anvil_state.txt"

func main() {
	// Setup context
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Anvil executable
	anvil := exec.Command(anvilCommand, "--host", "0.0.0.0", "--ipc")

	// Output pipe of the Anvil process
	stdout, err := anvil.StdoutPipe()
	if err != nil {
		panic(err)
	}

	// Start the Anvil process
	if err := anvil.Start(); err != nil {
		panic(err)
	}

	// Notifies that the anvil process has started
	start := make(chan struct{})

	// Print the output of the Anvil process
	go func() {
		var started bool
		scanner := bufio.NewScanner(stdout)

		for scanner.Scan() {
			m := scanner.Text()
			fmt.Println(m)

			// Notify the start channel that Anvil has started
			if strings.HasPrefix(m, startupMessage) && !started {
				close(start)
				started = true
			}
		}
	}()

	// Wait for the Anvil process to exit
	go func() {
		if err := anvil.Wait(); err != nil {
			panic(err)
		}
	}()

	// Kill the Anvil process on exit
	defer func() {
		if err := anvil.Process.Kill(); err != nil {
			panic(err)
		}
	}()

	// Wait for the Anvil process to start
	select {
	case <-start:
		fmt.Println("Started the Anvil process")
	case <-ctx.Done():
		return
	}

	// Connect to the Anvil ipc with a timeout
	dCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	c, err := rpc.DialContext(dCtx, ipcPath)
	if err != nil {
		panic(err)
	}
	client := ethclient.NewClient(c)
	defer client.Close()

	fmt.Println("Connected to the Anvil process")

	// Channels for communicating with the snapshot capture routine
	snapshotCh := make(chan uint64, 1)
	savedSnapshotCh := make(chan struct{})

	// Start the snapshot capture goroutine to capture and save snapshots to disk
	go func() {
		for {
			blockNumber := <-snapshotCh

			// Dump the Anvil state
			var result string
			err = c.Call(&result, "anvil_dumpState")
			if err != nil {
				panic(err)
			}

			// Write the Anvil state
			err = os.WriteFile(anvilState, []byte(result), 0644)
			if err != nil {
				panic(err)
			}

			fmt.Printf("Captured snapshot at block %d\n", blockNumber)

			// Notify that we have captured a snapshot
			savedSnapshotCh <- struct{}{}
		}
	}()

	// Load the saved Anvil state
	data, err := os.ReadFile(anvilState)

	if len(data) == 0 || err != nil {
		fmt.Println("No Anvil state found")

		// Capture a snapshot on startup if we don't have any saved state
		blockNumber, err := client.BlockNumber(ctx)
		if err != nil {
			panic(err)
		}
		snapshotCh <- blockNumber
		<-savedSnapshotCh
	} else {
		// Load the Anvil state
		var result bool
		err = c.Call(&result, "anvil_loadState", string(data))
		if err != nil {
			panic(err)
		}

		if result {
			fmt.Println("Loaded the Anvil state")
		} else {
			panic(errors.New("failed to load the Anvil state"))
		}
	}

	// Subscribe to new blocks
	newHeadCh := make(chan *types.Header)
	subscription, err := client.SubscribeNewHead(ctx, newHeadCh)
	if err != nil {
		panic(err)
	}
	defer subscription.Unsubscribe()

	fmt.Println("Subscribed to new blocks")

	var (
		snapshot          bool
		pendingSnapshot   uint64
		latestBlockNumber uint64
	)

	// Routine for taking snapshots as new blocks arrive
	for {
		select {
		case header := <-newHeadCh:
			latestBlockNumber = header.Number.Uint64()
			if snapshot {
				// If we are currently taking a snapshot, take a new one once it current one is done
				pendingSnapshot = latestBlockNumber
			} else {
				// Take a new snapshot
				snapshotCh <- latestBlockNumber
				snapshot = true
				pendingSnapshot = 0
			}
		case <-savedSnapshotCh:
			if pendingSnapshot == 0 {
				// The last snapshot has completed and there are no pending snapshots to take
				snapshot = false
			} else {
				// If we received new blocks after starting the last snapshot, take a new snapshot
				snapshotCh <- pendingSnapshot
				pendingSnapshot = 0
			}
		case err := <-subscription.Err():
			fmt.Printf("Subscription err: %v\n", err)
		case <-ctx.Done():
			// If we are currently taking a snapshot, drain the snapshot saved channel
			if snapshot {
				<-savedSnapshotCh
			}

			// Take a new snapshot
			snapshotCh <- latestBlockNumber
			<-savedSnapshotCh

			return
		}
	}
}
