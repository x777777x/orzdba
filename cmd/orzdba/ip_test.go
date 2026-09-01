package main

import (
	"net"
	"testing"
)

func TestIsLocalHostLoopback(t *testing.T) {
	for _, h := range []string{"", "127.0.0.1", "::1", "localhost"} {
		if !isLocalHost(h) {
			t.Errorf("isLocalHost(%q) = false, want true (loopback)", h)
		}
	}
}

func TestIsLocalHostRemote(t *testing.T) {
	for _, h := range []string{"192.168.99.99", "10.0.0.1", "8.8.8.8"} {
		if isLocalHost(h) {
			t.Errorf("isLocalHost(%q) = true, want false (remote IP)", h)
		}
	}
	// Hostnames are treated as remote (not resolved).
	if isLocalHost("db.internal.example") {
		t.Error("isLocalHost(hostname) = true, want false (hostname → remote)")
	}
}

func TestIsLocalHostOwnInterface(t *testing.T) {
	// If the machine has a non-loopback IPv4, isLocalHost must match it.
	ip := ownIPv4(t)
	if ip == "" {
		t.Skip("no non-loopback IPv4 on this host")
	}
	if !isLocalHost(ip) {
		t.Errorf("isLocalHost(%q) = false, want true (own interface IP)", ip)
	}
}

func TestMonitoredIP(t *testing.T) {
	// Local (loopback) → own primary IP.
	cfg := &config{host: "127.0.0.1", mysql: true}
	if got := monitoredIP(cfg); got == "" || got == "?" {
		t.Errorf("monitoredIP(local) = %q, want a real IP", got)
	}
	// Remote mysql → the -H address.
	cfg = &config{host: "192.168.99.99", mysql: true}
	if got := monitoredIP(cfg); got != "192.168.99.99" {
		t.Errorf("monitoredIP(remote) = %q, want 192.168.99.99", got)
	}
	// No mysql → own primary IP.
	cfg = &config{host: "192.168.99.99", mysql: false}
	if got := monitoredIP(cfg); got == "192.168.99.99" {
		t.Errorf("monitoredIP(no mysql) = %q, want own IP (not the -H value)", got)
	}
}

// ownIPv4 returns the first non-loopback IPv4 address of this host ("" if none).
func ownIPv4(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if v4 := ipnet.IP.To4(); v4 != nil {
			return v4.String()
		}
	}
	return ""
}
