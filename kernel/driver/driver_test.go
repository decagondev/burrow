package driver

import (
	"errors"
	"strings"
	"testing"

	"burrow/kernel/hal"
	"burrow/kernel/haltest"
	"burrow/kernel/klog"
)

type fake struct {
	name    string
	present bool
	err     error
	started *[]string
}

func (f fake) Name() string    { return f.name }
func (f fake) Probe(*Env) bool { return f.present }
func (f fake) Start(env *Env) error {
	env.Log.Infof("hello")
	*f.started = append(*f.started, f.name)
	return f.err
}

func TestStartAllOrderProbeAndErrors(t *testing.T) {
	var out strings.Builder
	var calls []string
	r := NewRegistry()
	r.Register(fake{"zeta", true, nil, &calls})
	r.Register(fake{"alpha", true, nil, &calls})
	r.Register(fake{"absent", false, nil, &calls})
	r.Register(fake{"broken", true, errors.New("no luck"), &calls})

	got := r.StartAll(&Env{Log: klog.New(&out, klog.Info)})
	if strings.Join(got, ",") != "alpha,zeta" {
		t.Errorf("started %v, want [alpha zeta]", got)
	}
	if strings.Join(calls, ",") != "alpha,broken,zeta" {
		t.Errorf("Start called for %v, want name order without absent", calls)
	}
	for _, want := range []string{
		"[driver] INFO: absent: no device",
		"[driver] ERROR: broken: no luck",
		"[alpha] INFO: hello",
		"[driver] INFO: started: [alpha zeta]",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, out.String())
		}
	}
}

func TestRegisterTwicePanics(t *testing.T) {
	r := NewRegistry()
	r.Register(fake{name: "x"})
	defer func() {
		if recover() == nil {
			t.Fatal("second Register did not panic")
		}
	}()
	r.Register(fake{name: "x"})
}

func TestServeHandlesThenAcks(t *testing.T) {
	q := haltest.NewFakeIRQ()
	handled := make(chan int, 3)
	n := 0
	var line hal.IRQLine = q
	done := Serve(line, func() { n++; handled <- n })
	for i := 1; i <= 3; i++ {
		q.Fire()
		if got := <-handled; got != i {
			t.Fatalf("handled %d, want %d", got, i)
		}
	}
	q.Close()
	<-done
	if q.Acks() != 3 {
		t.Fatalf("Acks = %d, want 3 (one per interrupt, after handle)", q.Acks())
	}
}
