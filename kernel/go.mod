module burrow/kernel

go 1.27.3

// gVisor (W17-S02) needs go 1.27.3 here; the runtime keeps the GODEBUG
// defaults the kernel was built and tested with since W6 (go 1.22: no
// prctl(PR_SET_VMA) naming of its mappings, which the shim does not serve).
godebug default=go1.22

require gvisor.dev/gvisor v0.0.0-20261104092019-e0105ccf200a

require (
	github.com/google/btree v1.1.2 // indirect
	golang.org/x/exp v0.0.0-20250711185948-6ae5c78190dc // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)
