/* Bitwise (table-free) CRC-32. ~8x slower than a table, but 15 lines and no
 * initialised data. Good enough for a few MB at boot. Optimise in a later slice. */
#include "crc32.h"

uint32_t crc32_ieee(const void *data, size_t len) {
    const uint8_t *p = data;
    uint32_t crc = 0xFFFFFFFFu;
    while (len--) {
        crc ^= *p++;
        for (int i = 0; i < 8; i++)
            crc = (crc >> 1) ^ (0xEDB88320u & (0u - (crc & 1u)));
    }
    return ~crc;
}
