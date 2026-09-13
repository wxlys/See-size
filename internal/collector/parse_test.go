package collector

import (
	"math"
	"testing"
)

func TestCPUPercent(t *testing.T) {
	previous := cpuTimes{total: 1000, idle: 700}
	current := cpuTimes{total: 1100, idle: 740}
	got := cpuPercent(previous, current)
	if math.Abs(got-60) > 0.001 {
		t.Fatalf("cpuPercent = %.2f, want 60", got)
	}
}

func TestParseMemInfo(t *testing.T) {
	values, err := parseMemInfo("MemTotal: 2048 kB\nMemAvailable: 1024 kB\nCached: 256 kB\n")
	if err != nil {
		t.Fatal(err)
	}
	if values["MemTotal"] != 2048*1024 || values["MemAvailable"] != 1024*1024 {
		t.Fatalf("unexpected values: %#v", values)
	}
}

func TestParseNetDev(t *testing.T) {
	input := "Inter-| Receive | Transmit\n lo: 10 0 0 0 0 0 0 0 20 0 0 0 0 0 0 0\n eth0: 100 0 0 0 0 0 0 0 250 0 0 0 0 0 0 0\n"
	rx, tx, err := parseNetDev(input)
	if err != nil {
		t.Fatal(err)
	}
	if rx != 100 || tx != 250 {
		t.Fatalf("rx=%d tx=%d, want 100 and 250", rx, tx)
	}
}

func TestParseOSRelease(t *testing.T) {
	values := parseOSRelease("NAME=Ubuntu\nVERSION_ID=\"24.04\"\n")
	if values["NAME"] != "Ubuntu" || values["VERSION_ID"] != "24.04" {
		t.Fatalf("unexpected values: %#v", values)
	}
}
