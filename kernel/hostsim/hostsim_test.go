package hostsim

import (
	"strings"
	"testing"
	"time"

	"burrow/kernel/console"
)

const wait = 2 * time.Second

func boot(t *testing.T) *Machine {
	t.Helper()
	m := New()
	if got := strings.Join(m.Start(), ","); got != "ps2,serial" {
		t.Fatalf("started %q, want ps2,serial", got)
	}
	t.Cleanup(m.Stop)
	if err := m.WaitFor(console.Prompt, wait); err != nil {
		t.Fatal(err)
	}
	return m
}

// The QEMU smoke test's keystrokes, on simulated hardware.
func TestKeyboardToConsole(t *testing.T) {
	m := boot(t)
	if err := m.TypeKeys("echo Hello, World!\n"); err != nil {
		t.Fatal(err)
	}
	if err := m.WaitFor(console.Prompt+"echo Hello, World!\nHello, World!\n", wait); err != nil {
		t.Fatal(err)
	}
}

func TestSerialToConsoleWithEditing(t *testing.T) {
	m := boot(t)
	if err := m.TypeSerial("ehco oops\x15echo fixed\x7fd\r\n"); err != nil {
		t.Fatal(err)
	}
	if err := m.WaitFor("\nfixed\n", wait); err != nil {
		t.Fatal(err)
	}
}

// Keyboard and serial share one input channel, in arrival order.
func TestBothInputsInterleave(t *testing.T) {
	m := boot(t)
	if err := m.TypeKeys("echo a"); err != nil {
		t.Fatal(err)
	}
	if err := m.WaitFor("echo a", wait); err != nil {
		t.Fatal(err)
	}
	if err := m.TypeSerial("b\r"); err != nil {
		t.Fatal(err)
	}
	if err := m.WaitFor("echo ab\nab\n", wait); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.Output(), "[ps2] INFO: keyboard ready on irq 1") {
		t.Errorf("driver log missing:\n%s", m.Output())
	}
}

func TestFilesFromTheKeyboard(t *testing.T) {
	m := boot(t)
	if err := m.TypeKeys("write /tmp/a one two\ncat /etc/motd /tmp/a\n"); err != nil {
		t.Fatal(err)
	}
	if err := m.WaitFor("Welcome to Burrow (hostsim).\none two\n", wait); err != nil {
		t.Fatal(err)
	}
}
