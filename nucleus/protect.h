/* protect.h - SMEP and SMAP on (W20-S02), see protect.c. */
#ifndef NUCLEUS_PROTECT_H
#define NUCLEUS_PROTECT_H

#define CR4_SMEP (1ull << 20)
#define CR4_SMAP (1ull << 21)

void protect_init(void);   /* after vmm_init; prints "[nucleus] protections: ..." */
int protect_smep(void);    /* 1 when on */
int protect_smap(void);
#endif
