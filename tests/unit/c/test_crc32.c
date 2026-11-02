#include "minitest.h"
#include "crc32.h"

TEST(crc32_standard_check_value) {
    /* "123456789" -> 0xCBF43926 is THE published check value for CRC-32/IEEE. */
    CHECK_EQ_U64(crc32_ieee("123456789", 9), 0xCBF43926u);
}
TEST(crc32_empty) { CHECK_EQ_U64(crc32_ieee("", 0), 0); }
