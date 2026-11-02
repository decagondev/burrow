package block_test

import (
	"testing"

	"burrow/kernel/block"
	"burrow/kernel/block/blocktest"
)

func TestRAMConformance(t *testing.T) {
	data := blocktest.Pattern(64, 512)
	blocktest.Run(t, block.NewRAM(data, 512), data)
}

func TestRegistry(t *testing.T) {
	block.Register("hdb", block.NewRAM(make([]byte, 1024), 512))
	block.Register("hda", block.NewRAM(make([]byte, 512), 512))
	if got := block.Names(); len(got) != 2 || got[0] != "hda" || got[1] != "hdb" {
		t.Errorf("Names = %v", got)
	}
	if d, ok := block.Get("hdb"); !ok || d.Sectors() != 2 {
		t.Errorf("Get(hdb) = %v, %v", d, ok)
	}
}
