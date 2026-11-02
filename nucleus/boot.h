/* boot.h - what the loader handed over: boot_info v2, the physmap and the
 * boot modules. Every other nucleus module asks here instead of keeping its
 * own copy of the boot_info pointer. */
#ifndef NUCLEUS_BOOT_H
#define NUCLEUS_BOOT_H
#include <stdint.h>
#include <burrow/boot_info.h>

/* Validates boot_info (panics if wrong) and remembers it. */
void boot_init(struct boot_info *bi);
const struct boot_info *boot_info(void);

/* Physical address -> pointer through the physmap (all RAM, NX). */
void *phys_to_virt(uint64_t pa);

const struct boot_module *boot_module_find(const char *name);   /* 0 if absent */
const struct boot_module *boot_modules(uint32_t *count);
const struct e820_entry  *boot_e820(uint32_t *count);
#endif
