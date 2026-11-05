/* uregs.c - a waiting user thread's registers for Go (W10; NUCLEUS_ABI
 * 0x1025-0x1027): read and write them (signals), start a thread from an
 * image (fork), move a thread to another address space (execve). Every
 * value Go writes is checked
 * here: the frame goes back to ring 3 through IRETQ, and the FXSAVE image
 * through FXRSTOR, both of which #GP in ring 0 on bad input. */
#include "fpu.h"
#include "uthread.h"
#include "uspace.h"
#include "vmm.h"
#include "sched.h"
#include "trap.h"
#include "gdt.h"
#include "errno.h"
#include "lib_string.h"

#define RFLAGS_USER   0x00240DD5ull   /* CF PF AF ZF SF TF DF OF AC ID */
#define RFLAGS_IF_ON  (1ull << 9)
#define RFLAGS_FIXED  (1ull << 1)
#define FX_MXCSR      24              /* offset in the FXSAVE area */
#define MXCSR_VALID   0x0000FFBFu     /* the bits FXRSTOR accepts (MXCSR_MASK default) */

static int user_addr(uint64_t a) { return a < USER_VA_TOP; }

static int valid(const struct uregs *r) {
    return user_addr(r->rip) && user_addr(r->rsp) && user_addr(r->fs_base);
}

static uint64_t clean_rflags(uint64_t v) { return (v & RFLAGS_USER) | RFLAGS_IF_ON | RFLAGS_FIXED; }

static void clean_fx(uint8_t *fx, const uint8_t *src) {
    memcpy(fx, src, 512);
    uint32_t mxcsr;
    memcpy(&mxcsr, fx + FX_MXCSR, 4);
    mxcsr &= MXCSR_VALID;
    memcpy(fx + FX_MXCSR, &mxcsr, 4);
}

static void to_frame(struct trap_frame *f, const struct uregs *r) {
    f->r15 = r->r15; f->r14 = r->r14; f->r13 = r->r13; f->r12 = r->r12;
    f->r11 = r->r11; f->r10 = r->r10; f->r9 = r->r9; f->r8 = r->r8;
    f->rbp = r->rbp; f->rdi = r->rdi; f->rsi = r->rsi; f->rdx = r->rdx;
    f->rcx = r->rcx; f->rbx = r->rbx; f->rax = r->rax;
    f->rip = r->rip; f->rflags = clean_rflags(r->rflags); f->rsp = r->rsp;
    f->cs = SEL_UCODE; f->ss = SEL_UDATA;
}

static void from_frame(struct uregs *r, const struct trap_frame *f) {
    r->r15 = f->r15; r->r14 = f->r14; r->r13 = f->r13; r->r12 = f->r12;
    r->r11 = f->r11; r->r10 = f->r10; r->r9 = f->r9; r->r8 = f->r8;
    r->rbp = f->rbp; r->rdi = f->rdi; r->rsi = f->rsi; r->rdx = f->rdx;
    r->rcx = f->rcx; r->rbx = f->rbx; r->rax = f->rax;
    r->rip = f->rip; r->rflags = f->rflags; r->rsp = f->rsp; r->pad = 0;
}

long uthread_regs(uint64_t tid, struct uregs *buf, int write) {
    uint64_t fl;
    struct thread *t = uthread_held(tid, &fl);
    long rc = 0;
    if (!t) rc = -EBUSY;
    else if (write && !valid(buf)) rc = -EINVAL;
    else if (write) {
        to_frame(t->uframe, buf);
        uint8_t legacy[FPU_LEGACY_BYTES];
        clean_fx(legacy, buf->fx);
        fpu_set_legacy(t->fx, legacy);   /* XRSTOR loads it (W20-S03) */
        thread_set_fs_base(t, buf->fs_base);
    } else {
        from_frame(buf, t->uframe);
        buf->fs_base = t->fs_base;
        memcpy(buf->fx, t->fx, sizeof buf->fx);   /* the legacy part, saved when it was switched out */
    }
    uthread_unlock(fl);
    return rc;
}

long uthread_create_from(uint64_t as, const struct uregs *buf) {
    struct addr_space *space = uspace_get(as);
    if (!space) return -EBADF;
    if (!valid(buf)) return -EINVAL;
    struct trap_frame f;
    memset(&f, 0, sizeof f);
    to_frame(&f, buf);
    static uint8_t fx[512] __attribute__((aligned(16)));   /* serialised by the caller (Go's dispatcher) */
    clean_fx(fx, buf->fx);
    uspace_thread_ref(as, +1);
    struct thread *t = thread_create_user("user", &f, buf->fs_base, space->pt.root, as, fx);
    if (!t) {
        uspace_thread_ref(as, -1);
        return -EAGAIN;
    }
    return (long)thread_id(t);
}

long uthread_set_space(uint64_t tid, uint64_t as) {
    struct addr_space *space = uspace_get(as);
    if (!space) return -EBADF;
    uint64_t fl;
    struct thread *t = uthread_held(tid, &fl);
    if (!t) { uthread_unlock(fl); return -EBUSY; }
    uint64_t old = t->uspace;
    t->uspace = as;
    t->cr3 = space->pt.root;              /* loaded when it is scheduled next */
    uthread_unlock(fl);
    uspace_thread_ref(as, +1);
    uspace_thread_ref(old, -1);
    return 0;
}
