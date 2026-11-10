package proc

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestCensusQt and TestCensusX11: every system call a census saw on Linux
// (tests/regression/census-qt.txt from census-qt.sh, census-x11.txt from
// x11-linux.sh) has a handler, unless its line names the slice that will
// add one ("# W15-S02 ...", "# W16-S02 ..."). A named call that already has
// a handler is an error too: the note is stale and should go.
func TestCensusQt(t *testing.T)  { checkCensus(t, "census-qt.txt") }
func TestCensusX11(t *testing.T) { checkCensus(t, "census-x11.txt") }
func TestCensusNet(t *testing.T) { checkCensus(t, "census-net.txt") } // W24-S01
func TestCensusSsh(t *testing.T) { checkCensus(t, "census-ssh.txt") } // W24-S02

func checkCensus(t *testing.T, file string) {
	f, err := os.Open("../../tests/regression/" + file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		text, note, _ := strings.Cut(sc.Text(), "#")
		fields := strings.Fields(text)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			t.Errorf("line %d: want \"name number\", got %q", line, sc.Text())
			continue
		}
		nr, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			t.Errorf("line %d: %v", line, err)
			continue
		}
		_, inTable := table[nr]
		_, inTTable := ttable[nr]
		served := inTable || inTTable
		later := strings.Contains(note, "W1")
		switch {
		case !served && !later:
			t.Errorf("%s (%d): the census saw it and nothing serves it; implement it or name the slice", fields[0], nr)
		case served && later:
			t.Errorf("%s (%d): served now; drop the %q note", fields[0], nr, strings.TrimSpace(note))
		}
	}
}
