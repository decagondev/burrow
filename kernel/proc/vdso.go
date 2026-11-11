package proc

// vdso.go - the vDSO in every process (W25-S02): the nucleus keeps a data
// page with the clock and frames with the vDSO's code (nu_vdso); a new
// program gets both mapped just above its stack, [vvar] then [vdso], read
// only (the code executable), and AT_SYSINFO_EHDR names the code, where
// musl and Go look for clock_gettime and friends. The pages are device
// memory: never freed, shared by fork, mapped again by execve. A nucleus
// without a vDSO (or a fake) maps nothing and programs make the system
// calls. Docs: docs/04-plan/waves/WAVE-25-speed.md.

// Where the vDSO goes in every address space: just above the stack.
const (
	VvarBase = StackTop            // the data page
	VdsoBase = StackTop + PageSize // the code, as many pages as it takes
)

// Vdso is where the nucleus keeps the vDSO (nu_vdso).
type Vdso struct {
	DataPA uint64   // the data page ([vvar]); 0 = no vDSO
	CodePA []uint64 // the code's pages ([vdso]), in order
}

// mapVdso maps the vDSO into mem and returns the code's address for
// AT_SYSINFO_EHDR, or 0 when the nucleus has none.
func (s *System) mapVdso(mem *Mem) (uint64, error) {
	if s.vdso.DataPA == 0 || len(s.vdso.CodePA) == 0 {
		return 0, nil
	}
	if err := mem.MapDevice(VvarBase, s.vdso.DataPA, PageSize, 0); err != nil {
		return 0, err
	}
	for i, pa := range s.vdso.CodePA {
		if err := mem.MapDevice(VdsoBase+uint64(i)*PageSize, pa, PageSize, ProtExec); err != nil {
			return 0, err
		}
	}
	return VdsoBase, nil
}

// vdsoMaxPages is the most code pages the nucleus hands out
// (VDSO_MAX_CODE_PAGES in include/burrow/vdso.h).
const vdsoMaxPages = 4

// vdsoName is the /proc/<pid>/maps name of a vDSO page, or "".
func vdsoName(va uint64) string {
	switch {
	case va == VvarBase:
		return "[vvar]"
	case va >= VdsoBase && va < VdsoBase+vdsoMaxPages*PageSize:
		return "[vdso]"
	}
	return ""
}
