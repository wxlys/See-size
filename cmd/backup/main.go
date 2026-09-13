package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/seesize/seesize/internal/backup"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func run() error {
	source := flag.String("source", "", "source database (required)")
	dir := flag.String("dir", ".data/backups", "backup directory")
	keep := flag.Int("keep", 7, "successful backups to keep (1..365)")
	interval := flag.Duration("interval", 0, "repeat delay; 0 means one backup, otherwise >=1m")
	restore := flag.String("restore-to", "", "restore to a NEW database path")
	verify := flag.Bool("verify", false, "only verify source database")
	flag.Parse()
	if *source == "" || *keep < 1 || *keep > 365 || *interval < 0 || (*interval > 0 && *interval < time.Minute) || (*restore != "" && (*verify || *interval != 0)) || (*verify && *interval != 0) {
		return fmt.Errorf("invalid backup options")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *verify {
		work, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		if err := backup.Verify(work, *source); err != nil {
			return err
		}
		fmt.Println("Database integrity and SeeSize schema verified")
		return nil
	}
	if *restore != "" {
		work, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		if err := backup.Copy(work, *source, *restore); err != nil {
			return err
		}
		fmt.Println("Verified database restored to", *restore)
		return nil
	}
	for {
		work, cancel := context.WithTimeout(ctx, 5*time.Minute)
		path, removed, err := backup.Create(work, *source, *dir, *keep)
		cancel()
		if err != nil {
			if *interval == 0 {
				return err
			}
			fmt.Fprintln(os.Stderr, "Backup failed:", err)
		} else {
			fmt.Println("Verified backup:", path, "expired backups removed:", removed)
		}
		if *interval == 0 {
			return nil
		}
		timer := time.NewTimer(*interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
