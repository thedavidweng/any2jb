package readiness_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/thedavidweng/any2jb/internal/readiness"
	"github.com/thedavidweng/any2jb/internal/session"
)

func TestParseProcNetRoutePrefersPhysicalDefaultGateway(t *testing.T) {
	table := "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
		"docker0\t00000000\t010011AC\t0003\t0\t0\t10\t00000000\t0\t0\t0\n" +
		"eth0\t00000000\t0101A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
		"wlan0\t00000000\t0101A8C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n"
	if got := readiness.ParseProcNetRoute(table); got != "eth0" {
		t.Fatalf("ParseProcNetRoute = %q, want eth0", got)
	}
}

func TestLinuxContainerInterfacesDoNotMakeSingleLANAmbiguous(t *testing.T) {
	o := readiness.Observation{
		Linux: &readiness.LinuxGadget{},
		Addresses: []readiness.Address{
			{Interface: "docker0", IPv4: "172.17.0.1"},
			{Interface: "virbr0", IPv4: "192.168.122.1"},
			{Interface: "tailscale0", IPv4: "100.64.0.2"},
			{Interface: "enp3s0", IPv4: "192.168.1.42"},
		},
	}
	checks, err := (readiness.Inspector{Observe: func(context.Context) readiness.Observation { return o }}).Inspect(context.Background(), session.Run{})
	if err != nil {
		t.Fatal(err)
	}
	if checks[0].State != session.CheckVerified || checks[0].LANIPv4 != "192.168.1.42" {
		t.Fatalf("unexpected network check: %+v", checks[0])
	}
}

func fakeLinuxTree(t *testing.T) (string, readiness.LinuxProbeRoots) {
	t.Helper()
	root := t.TempDir()
	proc := filepath.Join(root, "proc")
	udc := filepath.Join(root, "sys/class/udc")
	cfg := filepath.Join(root, "sys/kernel/config")
	mod := filepath.Join(root, "sys/module")
	bin := filepath.Join(root, "usr/bin")
	sbin := filepath.Join(root, "usr/sbin")
	for _, dir := range []string{filepath.Join(proc, "net"), udc, cfg, mod, bin, sbin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	route := filepath.Join(proc, "net/route")
	if err := os.WriteFile(route, []byte("Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\nwlan0\t00000000\t0101A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mounts := filepath.Join(proc, "mounts")
	if err := os.WriteFile(mounts, []byte("configfs "+cfg+" configfs rw,nosuid,nodev,noexec 0 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkexec := filepath.Join(bin, "pkexec")
	if err := os.WriteFile(pkexec, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sbin, "modprobe"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, readiness.LinuxProbeRoots{
		ProcNetRoute:  route,
		ProcMounts:    mounts,
		UDCRoot:       udc,
		ConfigRoot:    cfg,
		ModuleRoot:    mod,
		PkexecPath:    pkexec,
		ModprobeDirs:  []string{sbin},
		EUID:          1000,
		WritableCheck: func(string) bool { return false },
	}
}

func checkMap(checks []session.Check) map[string]session.Check {
	m := make(map[string]session.Check, len(checks))
	for _, c := range checks {
		m[c.ID] = c
	}
	return m
}

func TestLinuxNoUDCHostReportsUSBUnavailableWithoutBlockingNetwork(t *testing.T) {
	_, roots := fakeLinuxTree(t)
	inspector := readiness.Inspector{Observe: func(context.Context) readiness.Observation {
		o := readiness.Observation{
			Addresses:    []readiness.Address{{Interface: "wlan0", IPv4: "192.168.1.50"}},
			USBSupported: true,
		}
		readiness.ObserveLinux(roots, &o)
		return o
	}}
	checks, err := inspector.Inspect(context.Background(), session.Run{})
	if err != nil {
		t.Fatal(err)
	}
	byID := checkMap(checks)
	if byID["network"].State != session.CheckVerified || byID["network"].LANIPv4 != "192.168.1.50" {
		t.Fatalf("network blocked on no-UDC Linux host: %+v", byID["network"])
	}
	if byID["linux-udc"].State != session.RequiresAttention || byID["linux-udc"].GatesTransport != "usb-hid" {
		t.Fatalf("expected linux-udc attention gated to usb-hid: %+v", byID["linux-udc"])
	}
}

func TestLinuxUDCHostReadyAndConflictGuard(t *testing.T) {
	_, roots := fakeLinuxTree(t)
	if err := os.MkdirAll(filepath.Join(roots.UDCRoot, "fe980000.usb"), 0o755); err != nil {
		t.Fatal(err)
	}
	gadgetRoot := filepath.Join(roots.ConfigRoot, "usb_gadget")
	if err := os.MkdirAll(gadgetRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	inspect := func() map[string]session.Check {
		o := readiness.Observation{
			Addresses:    []readiness.Address{{Interface: "wlan0", IPv4: "192.168.1.50"}},
			USBSupported: true,
		}
		readiness.ObserveLinux(roots, &o)
		checks, err := (readiness.Inspector{Observe: func(context.Context) readiness.Observation { return o }}).Inspect(context.Background(), session.Run{})
		if err != nil {
			t.Fatal(err)
		}
		return checkMap(checks)
	}

	ready := inspect()
	for _, id := range []string{"network", "linux-udc", "linux-configfs", "linux-gadget-access"} {
		if ready[id].State != session.CheckVerified {
			t.Fatalf("%s not verified: %+v", id, ready[id])
		}
	}
	if ready["usb-controllers"].State != session.Available || ready["fe980000.usb"].State != session.Available || ready["fe980000.usb"].Controller != "fe980000.usb" {
		t.Fatalf("UDC controller not available: %+v %+v", ready["usb-controllers"], ready["fe980000.usb"])
	}
	var slice []session.Check
	for _, c := range ready {
		slice = append(slice, c)
	}
	if err := session.KeyboardPrerequisites(slice, "fe980000.usb"); err != nil {
		t.Fatalf("KeyboardPrerequisites rejected ready Linux UDC: %v", err)
	}

	// Simulate another program's ConfigFS gadget bound to fe980000.usb.
	other := filepath.Join(gadgetRoot, "other-kvm")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "UDC"), []byte("fe980000.usb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conflict := inspect()
	if conflict["linux-gadget-access"].State != session.RequiresAttention || conflict["fe980000.usb"].State != session.RequiresAttention {
		t.Fatalf("foreign gadget conflict not reported: %+v %+v", conflict["linux-gadget-access"], conflict["fe980000.usb"])
	}
	slice = slice[:0]
	for _, c := range conflict {
		slice = append(slice, c)
	}
	if err := session.KeyboardPrerequisites(slice, "fe980000.usb"); err == nil {
		t.Fatal("KeyboardPrerequisites allowed clobbering foreign gadget")
	}
}
