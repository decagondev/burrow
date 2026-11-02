/* test_main.c - the explicit list of every host unit test. Add yours here. */
#include "minitest.h"
int mt_failures, mt_checks;

TEST(crc32_standard_check_value); TEST(crc32_empty);
TEST(kprintf_plain); TEST(kprintf_decimal); TEST(kprintf_hex); TEST(kprintf_long);
TEST(kprintf_string); TEST(kprintf_char_pct); TEST(kprintf_pointer); TEST(kprintf_unknown);
TEST(kprintf_int_min); TEST(kprintf_no_sink_is_safe);
TEST(manifest_valid); TEST(manifest_bad_magic); TEST(manifest_bad_count);
TEST(manifest_too_small); TEST(manifest_overlap_stage2); TEST(manifest_find);
TEST(e820_usable_sum); TEST(e820_max_addr); TEST(e820_names);
TEST(elf_valid); TEST(elf_load_segment); TEST(elf_non_load_is_skipped);
TEST(elf_rejects); TEST(elf_rejects_segment_past_end);

int main(void) {
    puts("[crc32]");    RUN(crc32_standard_check_value); RUN(crc32_empty);
    puts("[kprintf]");  RUN(kprintf_plain); RUN(kprintf_decimal); RUN(kprintf_hex); RUN(kprintf_long);
                        RUN(kprintf_string); RUN(kprintf_char_pct); RUN(kprintf_pointer);
                        RUN(kprintf_unknown); RUN(kprintf_int_min); RUN(kprintf_no_sink_is_safe);
    puts("[manifest]"); RUN(manifest_valid); RUN(manifest_bad_magic); RUN(manifest_bad_count);
                        RUN(manifest_too_small); RUN(manifest_overlap_stage2); RUN(manifest_find);
    puts("[memmap]");   RUN(e820_usable_sum); RUN(e820_max_addr); RUN(e820_names);
    puts("[elf64]");    RUN(elf_valid); RUN(elf_load_segment); RUN(elf_non_load_is_skipped);
                        RUN(elf_rejects); RUN(elf_rejects_segment_past_end);
    printf("\n%d checks, %d failures\n", mt_checks, mt_failures);
    return mt_failures ? 1 : 0;
}
