// esp.go lists the files of a UEFI boot partition (W26-S01): the loader at
// the path firmware looks for on removable media, the manifest and every
// payload under burrow/. kernel/cmd/mkfat then makes the FAT32 volume and
// BuildGPT (gpt.go) the disk.
//
// The manifest is the BIOS disk's record, so the UEFI loader checks each
// payload's size and CRC exactly as the BIOS loader does; its LBAs are the
// BIOS layout's and the UEFI loader does not use them.
// Pure: bytes in, file names and bytes out. Docs: docs/04-plan/waves/WAVE-26-uefi.md.
package main

import "fmt"

const (
	ESPLoaderPath   = "EFI/BOOT/BOOTX64.EFI"
	ESPManifestPath = "burrow/manifest"
	ESPPayloadDir   = "burrow/"
)

// ESPFiles returns path -> contents for the boot partition.
func ESPFiles(efiApp []byte, payloads []Payload) (map[string][]byte, Manifest, error) {
	var m Manifest
	files := map[string][]byte{ESPLoaderPath: efiApp}
	lba := uint32(FirstPayloadLBA)
	for _, p := range payloads {
		if p.Name == "manifest" || files[ESPPayloadDir+p.Name] != nil {
			return nil, m, fmt.Errorf("duplicate or reserved payload name %q", p.Name)
		}
		e, err := NewEntry(p.Name, lba, p.Data)
		if err != nil {
			return nil, m, err
		}
		m.Entries = append(m.Entries, e)
		lba += e.Sectors
		files[ESPPayloadDir+p.Name] = p.Data
	}
	mf, err := m.MarshalBinary()
	if err != nil {
		return nil, m, err
	}
	files[ESPManifestPath] = mf
	return files, m, nil
}
