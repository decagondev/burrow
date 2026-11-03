package proc

import "encoding/binary"

// The Linux x86-64 signal frame, built on the user stack when a handler
// runs and read back by rt_sigreturn (arch/x86/include/uapi/asm/sigcontext.h,
// ucontext.h, siginfo.h):
//
//	rsp ->  pretcode (the SA_RESTORER trampoline)       8
//	        struct ucontext                             304
//	          uc_flags, uc_link, uc_stack(24)
//	          uc_mcontext = struct sigcontext           256
//	          uc_sigmask                                8
//	        struct siginfo                              128
//	below the frame, 64-byte aligned: the FXSAVE area (uc_mcontext.fpstate)
const (
	ucOffset     = 8
	ucMcontext   = 40 // in the ucontext
	ucSigmask    = 296
	ucSize       = 304
	infoOffset   = ucOffset + ucSize
	frameSize    = infoOffset + 128
	redZone      = 128
	fxSize       = 512
	scFPState    = 184 // in the sigcontext
	scEflags     = 136
	scCSGSFSSS   = 144
	scErr        = 152
	scTrapno     = 160
	scOldmask    = 168
	scCR2        = 176
	siAddrOffset = 16 // si_addr (faults) / si_pid (kill) in the siginfo
)

// sigcontext order: r8..r15, rdi, rsi, rbp, rbx, rdx, rax, rcx, rsp, rip.
func scRegs(r *Regs) []*uint64 {
	return []*uint64{&r.R8, &r.R9, &r.R10, &r.R11, &r.R12, &r.R13, &r.R14, &r.R15,
		&r.RDI, &r.RSI, &r.RBP, &r.RBX, &r.RDX, &r.RAX, &r.RCX, &r.RSP, &r.RIP}
}

// sigInfo describes why a signal came.
type sigInfo struct {
	signo, code int32
	pid         int32  // kill: the sender
	addr        uint64 // faults: the address
	what        string // faults: for the log ("page fault")
}

// pushFrame writes the frame for a handler of signal sig below r.RSP and
// returns the registers the handler starts with.
func (p *Process) pushFrame(r Regs, sig int, act sigaction, oldMask uint64, info sigInfo) (Regs, error) {
	fx := (r.RSP - redZone - fxSize) &^ 63
	sp := ((fx - frameSize) &^ 15) - 8 // as if a call pushed the return address
	if err := p.mem.CopyOut(fx, r.FX[:]); err != nil {
		return r, err
	}
	f := make([]byte, frameSize)
	le := binary.LittleEndian
	le.PutUint64(f[0:], act.restorer)
	mc := f[ucOffset+ucMcontext:]
	for i, reg := range scRegs(&r) {
		le.PutUint64(mc[8*i:], *reg)
	}
	le.PutUint64(mc[scEflags:], r.RFLAGS)
	le.PutUint64(mc[scCSGSFSSS:], 0x2B|0x23<<48) // cs, gs, fs, ss
	le.PutUint64(mc[scTrapno:], uint64(info.code))
	le.PutUint64(mc[scOldmask:], oldMask)
	le.PutUint64(mc[scCR2:], info.addr)
	le.PutUint64(mc[scFPState:], fx)
	le.PutUint64(f[ucOffset+ucSigmask:], oldMask)
	si := f[infoOffset:]
	le.PutUint32(si[0:], uint32(info.signo))
	le.PutUint32(si[8:], uint32(info.code))
	if info.addr != 0 {
		le.PutUint64(si[siAddrOffset:], info.addr)
	} else {
		le.PutUint32(si[siAddrOffset:], uint32(info.pid))
	}
	if err := p.mem.CopyOut(sp, f); err != nil {
		return r, err
	}
	h := r // keeps FS and the FPU image (the handler starts from the same state)
	h.RIP, h.RSP = act.handler, sp
	h.RDI, h.RSI, h.RDX, h.RAX = uint64(sig), sp+infoOffset, sp+ucOffset, 0
	h.RFLAGS &^= 1 << 10 // DF clear, as the ABI requires at a call
	return h, nil
}

// popFrame reads back the context rt_sigreturn restores: the registers and
// FPU state from the ucontext at rsp (the handler's `ret` popped pretcode),
// and the signal mask saved with them.
func (p *Process) popFrame(cur Regs) (Regs, uint64, error) {
	uc := make([]byte, ucSize)
	if err := p.mem.CopyIn(cur.RSP, uc); err != nil {
		return cur, 0, err
	}
	le := binary.LittleEndian
	r := cur
	mc := uc[ucMcontext:]
	for i, reg := range scRegs(&r) {
		*reg = le.Uint64(mc[8*i:])
	}
	r.RFLAGS = le.Uint64(mc[scEflags:])
	if fx := le.Uint64(mc[scFPState:]); fx != 0 {
		if err := p.mem.CopyIn(fx, r.FX[:]); err != nil {
			return cur, 0, err
		}
	}
	return r, le.Uint64(uc[ucSigmask:]), nil
}
