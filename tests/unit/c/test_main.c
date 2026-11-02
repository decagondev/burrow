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
TEST(palloc_bumps_from_floor); TEST(palloc_skips_reserved_ranges); TEST(palloc_jumps_holes);
TEST(palloc_avoids_overlapping_nonusable); TEST(palloc_aligns_unaligned_entries);
TEST(palloc_exhaustion_and_limit); TEST(palloc_reserved_table_full);
TEST(paging_maps_4k_pages); TEST(paging_maps_2m_pages); TEST(paging_mixes_page_sizes);
TEST(paging_user_bit_reaches_every_level); TEST(paging_rejects_bad_input);
TEST(paging_refuses_to_overwrite); TEST(paging_out_of_memory);
TEST(image_reads_and_verifies); TEST(image_detects_corruption);
TEST(image_rejects_small_dest_and_read_errors);

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
    puts("[palloc]");   RUN(palloc_bumps_from_floor); RUN(palloc_skips_reserved_ranges);
                        RUN(palloc_jumps_holes); RUN(palloc_avoids_overlapping_nonusable);
                        RUN(palloc_aligns_unaligned_entries); RUN(palloc_exhaustion_and_limit);
                        RUN(palloc_reserved_table_full);
    puts("[paging]");   RUN(paging_maps_4k_pages); RUN(paging_maps_2m_pages);
                        RUN(paging_mixes_page_sizes); RUN(paging_user_bit_reaches_every_level);
                        RUN(paging_rejects_bad_input); RUN(paging_refuses_to_overwrite);
                        RUN(paging_out_of_memory);
    puts("[image]");    RUN(image_reads_and_verifies); RUN(image_detects_corruption);
                        RUN(image_rejects_small_dest_and_read_errors);
    printf("\n%d checks, %d failures\n", mt_checks, mt_failures);
    return mt_failures ? 1 : 0;
}
