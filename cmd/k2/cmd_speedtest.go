package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/kaitu-io/k2/daemon"
)

func cmdSpeedtest(args []string) {
	fs := flag.NewFlagSet("speedtest", flag.ExitOnError)
	serverID := fs.String("server-id", "", "specific server ID for testing")
	fs.Parse(args)

	client := daemon.NewClient(daemon.DefaultAddr)

	// Start speedtest
	params := map[string]any{}
	if *serverID != "" {
		params["server_id"] = *serverID
	}

	resp, err := client.Do("speedtest", params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if resp.Code != 0 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Message)
		os.Exit(1)
	}

	fmt.Println("Speed test started...")

	// Poll for results
	for i := 0; i < 60; i++ {
		time.Sleep(2 * time.Second)

		resp, err := client.Do("get_speedtest_status", nil)
		if err != nil {
			continue
		}

		data, _ := json.Marshal(resp.Data)
		var status struct {
			State    string  `json:"state"`
			Download float64 `json:"download_mbps"`
			Upload   float64 `json:"upload_mbps"`
			Latency  float64 `json:"latency_ms"`
			Error    string  `json:"error"`
		}
		json.Unmarshal(data, &status)

		switch status.State {
		case "completed":
			fmt.Printf("Download: %.1f Mbps\n", status.Download)
			fmt.Printf("Upload:   %.1f Mbps\n", status.Upload)
			fmt.Printf("Latency:  %.0f ms\n", status.Latency)
			return
		case "error":
			fmt.Fprintf(os.Stderr, "Speed test failed: %s\n", status.Error)
			os.Exit(1)
		case "running":
			// still running, continue polling
		}
	}

	fmt.Fprintf(os.Stderr, "Speed test timed out\n")
	os.Exit(1)
}
