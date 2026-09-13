// enroll exchanges a one-time registration code for a private Agent token file.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

func run() error {
	hub := flag.String("hub", "http://127.0.0.1:8080", "Hub URL")
	id := flag.String("id", "", "Agent ID")
	out := flag.String("out", "agent.token", "new credential file (must not exist)")
	flag.Parse()
	code := strings.TrimSpace(os.Getenv("SEE_SIZE_ENROLL_CODE"))
	if *id == "" || code == "" {
		return fmt.Errorf("id and SEE_SIZE_ENROLL_CODE required")
	}
	// Reserve the target before consuming the one-time code; never overwrite.
	file, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	body, _ := json.Marshal(map[string]string{"agent_id": *id, "code": code})
	client := http.Client{Timeout: 10 * time.Second}
	res, err := client.Post(strings.TrimRight(*hub, "/")+"/api/v1/agents/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("registration failed; empty output reserved, use new filename on retry: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 201 {
		return fmt.Errorf("registration returned %s; output remains empty", res.Status)
	}
	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return err
	}
	if result.Token == "" {
		return fmt.Errorf("missing token")
	}
	if _, err = file.WriteString(result.Token); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	fmt.Println("Device registered; credential saved to", *out)
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
