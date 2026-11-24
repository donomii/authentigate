package main

import (
	"net"
	"testing"
)

func TestInterfaceIPv4AddressesParsesCIDRAndExcludesLoopback(t *testing.T) {
	addresses := []net.Addr{
		&net.IPNet{IP: net.ParseIP("192.0.2.10"), Mask: net.CIDRMask(24, 32)},
		&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)},
		&net.IPNet{IP: net.ParseIP("2001:db8::1"), Mask: net.CIDRMask(64, 128)},
	}
	got := interfaceIPv4Addresses(addresses)
	if len(got) != 1 || got[0] != "192.0.2.10" {
		t.Fatalf("IPv4 interface addresses: %v; expected [192.0.2.10]", got)
	}
	if got := interfaceIPv4Addresses(nil); len(got) != 0 {
		t.Fatalf("no interfaces must produce no addresses, received %v", got)
	}
}
