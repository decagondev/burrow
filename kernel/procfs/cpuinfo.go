package procfs

// cpuinfo.go - /proc/cpuinfo: the CPU as CPUID describes it, decoded here
// from the raw registers (the caller passes the instruction, so this is
// host-tested with a table), one block per CPU online. Flags use Linux's
// names, in the order of its feature words. Never executes CPUID itself.
// Docs: WAVE-23-proc-virtio.md.

import (
	"fmt"
	"strings"
)

// CPU is what cpuinfo says of every CPU (they are all alike under QEMU).
type CPU struct {
	Vendor, Name        string
	Family, Model, Step uint32
	Level               uint32 // the highest standard leaf
	Flags               []string
	PhysBits, VirtBits  uint32
}

// CPUIDFunc runs CPUID for a leaf and subleaf.
type CPUIDFunc func(leaf, sub uint32) (a, b, c, d uint32)

// feature words: which register of which leaf, and its bits' names
var featureWords = []struct {
	leaf uint32
	reg  int // 0 a, 1 b, 2 c, 3 d
	bits map[int]string
}{
	{1, 3, map[int]string{0: "fpu", 1: "vme", 2: "de", 3: "pse", 4: "tsc", 5: "msr", 6: "pae", 7: "mce", 8: "cx8", 9: "apic",
		11: "sep", 12: "mtrr", 13: "pge", 14: "mca", 15: "cmov", 16: "pat", 17: "pse36", 19: "clflush", 23: "mmx",
		24: "fxsr", 25: "sse", 26: "sse2", 27: "ss", 28: "ht"}},
	{0x80000001, 3, map[int]string{11: "syscall", 20: "nx", 26: "pdpe1gb", 27: "rdtscp", 29: "lm"}},
	{1, 2, map[int]string{0: "pni", 1: "pclmulqdq", 3: "monitor", 9: "ssse3", 12: "fma", 13: "cx16", 17: "pcid",
		19: "sse4_1", 20: "sse4_2", 21: "x2apic", 22: "movbe", 23: "popcnt", 24: "tsc_deadline_timer", 25: "aes",
		26: "xsave", 28: "avx", 29: "f16c", 30: "rdrand", 31: "hypervisor"}},
	{0x80000001, 2, map[int]string{0: "lahf_lm", 5: "abm", 8: "3dnowprefetch"}},
	{7, 1, map[int]string{0: "fsgsbase", 1: "tsc_adjust", 3: "bmi1", 4: "hle", 5: "avx2", 7: "smep", 8: "bmi2",
		9: "erms", 10: "invpcid", 11: "rtm", 16: "avx512f", 18: "rdseed", 19: "adx", 20: "smap", 23: "clflushopt",
		24: "clwb", 29: "sha_ni"}},
}

// DecodeCPU reads the CPU's description through cpuid.
func DecodeCPU(cpuid CPUIDFunc) CPU {
	var c CPU
	level, b, cx, d := cpuid(0, 0)
	c.Level = level
	c.Vendor = regString(b, d, cx)
	maxExt, _, _, _ := cpuid(0x80000000, 0)
	sig, _, _, _ := cpuid(1, 0)
	c.Step = sig & 0xF
	c.Model = (sig>>4)&0xF | (sig>>16&0xF)<<4
	c.Family = (sig >> 8) & 0xF
	if c.Family == 0xF {
		c.Family += (sig >> 20) & 0xFF
	}
	if maxExt >= 0x80000004 {
		var name strings.Builder
		for leaf := uint32(0x80000002); leaf <= 0x80000004; leaf++ {
			a, b, cx, d := cpuid(leaf, 0)
			name.WriteString(regString(a, b, cx, d))
		}
		c.Name = strings.TrimSpace(strings.TrimRight(name.String(), "\x00"))
	}
	if maxExt >= 0x80000008 {
		a, _, _, _ := cpuid(0x80000008, 0)
		c.PhysBits, c.VirtBits = a&0xFF, (a>>8)&0xFF
	}
	for _, w := range featureWords {
		if (w.leaf < 0x80000000 && w.leaf > level) || (w.leaf >= 0x80000000 && w.leaf > maxExt) {
			continue
		}
		var r [4]uint32
		r[0], r[1], r[2], r[3] = cpuid(w.leaf, 0)
		for bit := 0; bit < 32; bit++ {
			if name, ok := w.bits[bit]; ok && r[w.reg]&(1<<bit) != 0 {
				c.Flags = append(c.Flags, name)
			}
		}
	}
	return c
}

// regString is registers' bytes as text, little-endian, in order.
func regString(regs ...uint32) string {
	b := make([]byte, 0, 4*len(regs))
	for _, r := range regs {
		b = append(b, byte(r), byte(r>>8), byte(r>>16), byte(r>>24))
	}
	return string(b)
}

// cpuinfo is a block per CPU, separated by blank lines.
func cpuinfo(c CPU, n int) string {
	var b strings.Builder
	for i := 0; i < max(1, n); i++ {
		for _, kv := range [][2]string{
			{"processor", fmt.Sprint(i)}, {"vendor_id", c.Vendor}, {"cpu family", fmt.Sprint(c.Family)},
			{"model", fmt.Sprint(c.Model)}, {"model name", c.Name}, {"stepping", fmt.Sprint(c.Step)},
			{"physical id", "0"}, {"siblings", fmt.Sprint(n)}, {"core id", fmt.Sprint(i)},
			{"cpu cores", fmt.Sprint(n)}, {"apicid", fmt.Sprint(i)}, {"fpu", "yes"}, {"fpu_exception", "yes"},
			{"cpuid level", fmt.Sprint(c.Level)}, {"wp", "yes"}, {"flags", strings.Join(c.Flags, " ")},
			{"clflush size", "64"}, {"cache_alignment", "64"},
			{"address sizes", fmt.Sprintf("%d bits physical, %d bits virtual", c.PhysBits, c.VirtBits)},
			{"power management", ""},
		} {
			tabs := "\t"
			if len(kv[0]) < 8 {
				tabs = "\t\t"
			}
			fmt.Fprintf(&b, "%s%s: %s\n", kv[0], tabs, kv[1])
		}
		b.WriteString("\n")
	}
	return b.String()
}
