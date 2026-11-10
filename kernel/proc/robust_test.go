package proc_test

import (
	"syscall"
	"testing"
)

// set_robust_list (W24-S02): the head's length is checked, as on Linux.
func TestSetRobustList(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	if e := r.call(p, 273, scratch, 24); e != 0 {
		t.Errorf("set_robust_list = %d", e)
	}
	if e := r.call(p, 273, scratch, 16); e != -int64(syscall.EINVAL) {
		t.Errorf("a wrong length = %d", e)
	}
}
