package proc_test

import (
	"syscall"
	"testing"
)

// TestNamedDatagrams: syslog's shape (W23-S06): a server binds a datagram
// socket to a path, clients send to it by name or after connect, and
// recvfrom names the sender.
func TestNamedDatagrams(t *testing.T) {
	r, _ := vfsRig(t)
	p := r.spawn()
	srv := uint64(r.call(p, 41, 1, sockDgram, 0))
	if e := r.call(p, 49, srv, scratch+3100, r.addr(p, scratch+3100, "/tmp/log")); e != 0 {
		t.Fatalf("bind = %d", e)
	}
	if r.mode(p, 4, "/tmp/log")&syscall.S_IFMT != syscall.S_IFSOCK {
		t.Error("no socket node at /tmp/log")
	}
	named := uint64(r.call(p, 41, 1, sockDgram, 0)) // a client with a name of its own
	r.call(p, 49, named, scratch+3300, r.addr(p, scratch+3300, "\x00client"))
	r.str(p, scratch, "hello")
	if n := r.call(p, 44, named, scratch, 5, 0, scratch+3100, r.addr(p, scratch+3100, "/tmp/log")); n != 5 {
		t.Fatalf("sendto = %d", n)
	}
	anon := uint64(r.call(p, 41, 1, sockDgram, 0)) // logger's shape: connect, then send
	if e := r.call(p, 42, anon, scratch+3100, r.addr(p, scratch+3100, "/tmp/log")); e != 0 {
		t.Fatalf("connect = %d", e)
	}
	r.str(p, scratch, "second")
	if n := r.call(p, 1, anon, scratch, 6); n != 6 {
		t.Fatalf("write after connect = %d", n)
	}
	r.put(p, scratch+600, []byte{110, 0, 0, 0})
	if n := r.call(p, 45, srv, scratch+100, 64, 0, scratch+500, scratch+600); n != 5 || string(read(p, scratch+100, 5)) != "hello" ||
		string(read(p, scratch+502, 7)) != "\x00client" {
		t.Errorf("first recvfrom = %d %q from %q", n, read(p, scratch+100, 5), read(p, scratch+502, 7))
	}
	r.put(p, scratch+600, []byte{110, 0, 0, 0})
	if n := r.call(p, 45, srv, scratch+100, 64, 0, scratch+500, scratch+600); n != 6 || le.Uint32(read(p, scratch+600, 4)) != 0 {
		t.Errorf("second recvfrom = %d, an unnamed sender's address is %d bytes (want 0)", n, le.Uint32(read(p, scratch+600, 4)))
	}
	if n := r.call(p, 45, srv, scratch+100, 64, msgDontwait, 0, 0); n != -int64(syscall.EAGAIN) {
		t.Errorf("an empty mailbox = %d", n)
	}
}

func TestDatagramRefusals(t *testing.T) {
	r, _ := vfsRig(t)
	p := r.spawn()
	c := uint64(r.call(p, 41, 1, sockDgram, 0))
	r.str(p, scratch, "x")
	if e := r.call(p, 1, c, scratch, 1); e != -int64(syscall.ENOTCONN) {
		t.Errorf("send with no peer = %d", e)
	}
	if e := r.call(p, 44, c, scratch, 1, 0, scratch+3100, r.addr(p, scratch+3100, "\x00nobody")); e != -int64(syscall.ECONNREFUSED) {
		t.Errorf("sendto a name nobody holds = %d", e)
	}
	l := r.listener(p, "/tmp/stream", 1)
	_ = l
	if e := r.call(p, 44, c, scratch, 1, 0, scratch+3100, r.addr(p, scratch+3100, "/tmp/stream")); e != -int64(syscall.EPROTOTYPE) {
		t.Errorf("sendto a stream socket = %d", e)
	}
	srv := uint64(r.call(p, 41, 1, sockDgram, 0))
	r.call(p, 49, srv, scratch+3100, r.addr(p, scratch+3100, "\x00full"))
	r.put(p, scratch+700, []byte{0, 16, 0, 0}) // SO_RCVBUF 4096 (Linux doubles it: room for 8192)
	r.call(p, 54, srv, 1, 8, scratch+700, 4)
	big := make([]byte, 3000)
	r.put(p, scratch+1000, big)
	sent := 0
	for i := 0; i < 10; i++ {
		if n := r.call(p, 44, c, scratch+1000, 3000, msgDontwait, scratch+3100, r.addr(p, scratch+3100, "\x00full")); n == 3000 {
			sent++
		} else if n != -int64(syscall.EAGAIN) {
			t.Fatalf("sendto a full mailbox = %d", n)
		}
	}
	if sent == 0 || sent == 10 {
		t.Errorf("%d of 10 datagrams fit a small receive buffer", sent)
	}
	r.call(p, 3, srv) // closed: its name is gone
	if e := r.call(p, 44, c, scratch, 1, 0, scratch+3100, r.addr(p, scratch+3100, "\x00full")); e != -int64(syscall.ECONNREFUSED) {
		t.Errorf("sendto a closed socket = %d", e)
	}
}
