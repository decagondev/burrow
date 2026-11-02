package klog

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestFormatAndLevelFilter(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, Info)
	l.Debugf("hidden")
	l.Infof("boot %d", 1)
	l.With("vfs").Warnf("slow")
	want := "[kernel] INFO: boot 1\n[vfs] WARN: slow\n"
	if buf.String() != want {
		t.Fatalf("got %q want %q", buf.String(), want)
	}
}

func TestConcurrentLinesDoNotInterleave(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, Debug)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); l.With("t").Infof("xxxxxxxxxxxxxxxxxxxx") }()
	}
	wg.Wait()
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line != "[t] INFO: xxxxxxxxxxxxxxxxxxxx" {
			t.Fatalf("interleaved line: %q", line)
		}
	}
}
