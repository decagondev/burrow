/* pat.h - the page attribute table (W21-S04): entry 1 becomes write-
 * combining on every CPU, so a page mapped with PWT alone (and PCD, PAT
 * clear) is WC; entries 0, 2 and 3 keep their reset meaning (WB, UC-, UC),
 * so every mapping made before keeps its type. Without PAT there is no WC:
 * a WC request maps uncached. Intel SDM vol. 3 §11.12; ADR-0011's per-CPU
 * start-up runs pat_init_cpu on each AP. */
#ifndef NUCLEUS_PAT_H
#define NUCLEUS_PAT_H
#include <stdint.h>

#define MSR_PAT        0x277u
#define PAT_TYPE_WC    0x01u
#define PAT_WC_INDEX   1u       /* PWT=1, PCD=0, PAT=0 */

void pat_init(void);            /* the BSP, after vmm_init; prints the marker */
void pat_init_cpu(void);        /* each AP, before it maps anything */
int  pat_has_wc(void);
/* PTE bits for a write-combining page: PWT, or PCD|PWT (uncached) without PAT. */
uint64_t pat_wc_flags(void);
#endif
