/* trap.h - IDT, exception reporting, IRQ dispatch and fault fixups.
 *
 * Every vector enters through trap.asm, which saves all registers as a
 * struct trap_frame and moves it from the IST stack onto the current
 * thread's kernel stack (see percpu.h), so a handler may block or switch
 * threads. Exceptions with no fixup print a register dump and panic.
 *
 * Fixups: an instruction listed in the .fixup section (asm: probe.asm) may
 * fault; the handler then resumes at its listed resume address and records
 * what happened in trap_last. The Go kernel's copyin/copyout will use this. */
#ifndef NUCLEUS_TRAP_H
#define NUCLEUS_TRAP_H
#include <stdint.h>

struct trap_frame {
    uint64_t r15, r14, r13, r12, r11, r10, r9, r8;
    uint64_t rbp, rdi, rsi, rdx, rcx, rbx, rax;
    uint64_t vector, error;
    uint64_t rip, cs, rflags, rsp, ss;     /* pushed by the CPU */
};
_Static_assert(sizeof(struct trap_frame) == 22 * 8, "trap.asm FRAME_SIZE");

#define VEC_UD          6
#define VEC_DF          8
#define VEC_GP         13
#define VEC_PF         14
#define VEC_IRQ_BASE   0x20   /* 8259 remapped here, then masked (W5-S05) */
#define VEC_SPURIOUS   0xFF

struct trap_record { uint64_t count, vector, error, cr2, rip; };
extern volatile struct trap_record trap_last;   /* last fault resolved by a fixup */

typedef void (*irq_handler)(struct trap_frame *f);

void trap_init(void);
void trap_set_handler(uint8_t vector, irq_handler h);   /* vectors >= 32 only */
const char *trap_name(uint64_t vector);

/* probe.asm: touch memory / execute ud2, report a fault instead of panicking. */
int probe_read64(uint64_t addr, uint64_t *out);   /* 0 ok, -1 faulted */
int probe_write64(uint64_t addr, uint64_t value); /* 0 ok, -1 faulted */
int probe_ud2(void);                              /* always -1 (it always faults) */
int probe_exec(uint64_t addr);                    /* 0 ran (a ret), -1 fetching it faulted (W20-S02) */
#endif
