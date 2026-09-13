// scan is an opt-in one-shot companion to the continuously running Agent.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/seesize/seesize/internal/disk"
	"net/http"
	"os"
	"strings"
	"time"
)

func run() error {
	root := flag.String("root", "", "explicit directory to scan")
	id := flag.String("id", os.Getenv("SEE_SIZE_AGENT_ID"), "existing Agent ID")
	hub := flag.String("hub", os.Getenv("SEE_SIZE_HUB_URL"), "Hub URL; omit for local JSON")
	token := flag.String("token", os.Getenv("SEE_SIZE_AGENT_TOKEN"), "Agent credential")
	tokenFile := flag.String("token-file", "", "read Agent credential from protected file")
	depth := flag.Int("depth", 3, "directory display depth (0..5)")
	limit := flag.Int("max-entries", 20000, "maximum entries (up to 100000)")
	rate := flag.Int("rate", 1000, "metadata entries per second (1..100000)")
	timeout := flag.Duration("timeout", 15*time.Second, "scan duration limit")
	flag.Parse()
	if *tokenFile != "" {
		body, err := os.ReadFile(*tokenFile)
		if err != nil {
			return err
		}
		*token = strings.TrimSpace(string(body))
	}
	if *root == "" || *timeout <= 0 || *timeout > time.Minute {
		return fmt.Errorf("explicit root and timeout in (0,1m] required")
	}
	if *hub != "" && (*id == "" || *token == "") {
		return fmt.Errorf("upload requires id and token")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	snapshot, err := disk.ScanRate(ctx, *root, *depth, *limit, *rate)
	if err != nil {
		return err
	}
	snapshot.AgentID = *id
	body, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if *hub != "" {
		req, err := http.NewRequest(http.MethodPost, strings.TrimRight(*hub, "/")+"/api/v1/agents/disk-snapshots", bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+*token)
		req.Header.Set("Content-Type", "application/json")
		client := http.Client{Timeout: 10 * time.Second}
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusAccepted {
			return fmt.Errorf("upload returned %s", res.Status)
		}
	}
	fmt.Println(string(body))
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
