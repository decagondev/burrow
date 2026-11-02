#include "ktest.h"
#include "kprintf.h"
#include "lib_string.h"
#include "panic.h"
#include "io.h"

struct ktest { const char *name; int (*fn)(void); };

static const struct ktest g_tests[] = {
    { "trap_ud_fixup",      kt_trap_ud_fixup },
    { "trap_gp_fixup",      kt_trap_gp_fixup },
    { "trap_pf_fixup",      kt_trap_pf_fixup },
    { "trap_unhandled_ud",  kt_trap_unhandled_ud },
    { "pmm_alloc_all_free_all", kt_pmm_alloc_all_free_all },
    { "vmm_map_write_unmap", kt_vmm_map_write_unmap },
    { "vmm_protect_readonly", kt_vmm_protect_readonly },
    { "vmm_kernel_wx",      kt_vmm_kernel_wx },
    { "vm_reserve_commit",  kt_vm_reserve_commit },
    { "vm_reserve_hint",    kt_vm_reserve_hint },
    { "lapic_self_ipi",     kt_lapic_self_ipi },
    { "time_tick_rate",     kt_time_tick_rate },
    { "sched_ping_pong",    kt_sched_ping_pong },
    { "sched_preempts",     kt_sched_preempts },
    { "sched_sleep_and_fs_base", kt_sched_sleep_and_fs_base },
    { "sched_stack_guard",  kt_sched_stack_guard },
    { "sse_state_per_thread", kt_sse_state_per_thread },
    { "spinlock_counter",   kt_spinlock_counter },
    { "futex_ping_pong",    kt_futex_ping_pong },
    { "futex_timeout_and_eagain", kt_futex_timeout_and_eagain },
    { "syscall_getpid",     kt_syscall_getpid },
    { "syscall_write",      kt_syscall_write },
    { "shim_unknown_once",  kt_shim_unknown_once },
};

static const char *g_current;

void ktest_fail(const char *file, int line, const char *what) {
    kprintf("[ktest] %s FAIL: %s:%d: %s\n", g_current, file, line, what);
}

static const struct ktest *find(const char *name, uint64_t len) {
    for (unsigned i = 0; i < sizeof g_tests / sizeof g_tests[0]; i++)
        if (strlen(g_tests[i].name) == len && memcmp(g_tests[i].name, name, len) == 0) return &g_tests[i];
    return 0;
}

static int is_space(char c) { return c == ' ' || c == '\t' || c == '\n' || c == '\r'; }

void ktest_run(const char *text, uint64_t len) {
    unsigned passed = 0;
    for (uint64_t i = 0; i < len;) {
        while (i < len && is_space(text[i])) i++;
        uint64_t start = i;
        while (i < len && !is_space(text[i])) i++;
        if (i == start) break;
        if (text[start] == '#') {                       /* comment to end of line */
            while (i < len && text[i] != '\n') i++;
            continue;
        }
        const struct ktest *t = find(text + start, i - start);
        if (!t) {                                       /* kprintf has no %.*s */
            kprintf("[ktest] unknown test '");
            for (uint64_t k = start; k < i; k++) kprintf("%c", text[k]);
            kprintf("'\n");
            panic("unknown ktest name");
        }
        g_current = t->name;
        if (t->fn() != 0) panic("ktest %s failed", t->name);
        kprintf("[ktest] %s ok\n", t->name);
        passed++;
    }
    kprintf("[ktest] %u passed\n", passed);
    kprintf("BURROW:KTEST:OK\n");
    qemu_exit(QEMU_EXIT_SUCCESS);
    cpu_halt_forever();
}
