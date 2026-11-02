#include "spinlock.h"
#include "panic.h"

void spin_deadlock(const struct spinlock *l) {
    panic("spinlock '%s' never released (deadlock or re-entry)", l->name ? l->name : "?");
}
