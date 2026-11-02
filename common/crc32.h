/* crc32.h - IEEE 802.3 CRC-32 (same as Go's hash/crc32.ChecksumIEEE). */
#ifndef COMMON_CRC32_H
#define COMMON_CRC32_H
#include <stdint.h>
#include <stddef.h>
uint32_t crc32_ieee(const void *data, size_t len);
#endif
