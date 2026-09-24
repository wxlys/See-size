package collector

import (
	"testing"
	"time"
)

func TestNetworkSelectionLifecycle(t *testing.T) {
	c := New()
	if err := c.SetNetworkInterfaces("eth0, eth0"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	input := func(rx, tx string) string {
		return "eth0: " + rx + " 0 0 0 0 0 0 0 " + tx + "\nveth1: 90000 0 0 0 0 0 0 0 90000\n"
	}
	first, err := c.network.sample(input("100", "200"), now)
	if err != nil || first.ReceivedBytes != 100 || first.ReceivedBytesPerSecond != 0 || len(first.Interfaces) != 1 {
		t.Fatalf("baseline %+v %v", first, err)
	}
	next, err := c.network.sample(input("200", "400"), now.Add(10*time.Second))
	if err != nil || next.RateUnavailable || next.ReceivedBytesPerSecond != 10 || next.SentBytesPerSecond != 20 {
		t.Fatalf("rates %+v %v", next, err)
	}
	missing, err := c.network.sample("", now.Add(20*time.Second))
	if err != nil || len(missing.MissingInterfaces) != 1 || missing.ReceivedBytesPerSecond != 0 {
		t.Fatal("missing", missing, err)
	}
	back, err := c.network.sample(input("100000", "200000"), now.Add(30*time.Second))
	if err != nil || !back.RateUnavailable || back.ReceivedBytesPerSecond != 0 {
		t.Fatal("reappearing spike", back, err)
	}
	reset, err := c.network.sample(input("1", "2"), now.Add(40*time.Second))
	if err != nil || reset.ReceivedBytesPerSecond != 0 || reset.SentBytesPerSecond != 0 {
		t.Fatal("reset spike", reset, err)
	}
	if _, err := c.network.sample(input("bad", "2"), now.Add(50*time.Second)); err == nil {
		t.Fatal("invalid counter accepted")
	}
	if _, err := c.network.sample(input("100", "200"), now.Add(60*time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"lo", "eth0,", "eth 0", "../eth0"} {
		if c.SetNetworkInterfaces(bad) == nil {
			t.Fatal("invalid interface accepted", bad)
		}
	}
}

func TestNetworkAllInterfacesNoTopologySpike(t *testing.T) {
	n := networkState{}
	now := time.Now()
	n.sample("eth0: 100 0 0 0 0 0 0 0 100", now)
	got, err := n.sample("lo: 9999 0 0 0 0 0 0 0 9999\neth0: 200 0 0 0 0 0 0 0 200\nveth0: 999999 0 0 0 0 0 0 0 999999", now.Add(10*time.Second))
	if err != nil || got.Scope != "all-non-loopback" || len(got.Interfaces) != 2 || got.ReceivedBytesPerSecond != 10 {
		t.Fatalf("new interface spike %+v %v", got, err)
	}
}
