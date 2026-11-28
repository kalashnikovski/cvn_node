package node

import (
	"os"
	"path/filepath"
	"testing"
)

// startedInAnEmptyFolder moves the test into a new, empty folder, as a seed
// runs in the folder of its chain, and moves back when the test ends.
func startedInAnEmptyFolder(t *testing.T) string {
	t.Helper()
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	if err := os.Chdir(folder); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(before) })
	return folder
}

func programIsIn(t *testing.T, folder string) {
	t.Helper()
	saved := programFolder
	programFolder = func() string { return folder }
	t.Cleanup(func() { programFolder = saved })
}

// A seed set up by deploy/install_seed.sh runs in /var/lib/cvn_seed/NETWORK,
// and the page is with the program in /opt/cvn_node. Every seed showed one
// bare line in place of the explorer until the node looked there too.
func TestTheExplorerPageIsFoundBesideTheProgramWhenTheNodeRunsElsewhere(t *testing.T) {
	startedInAnEmptyFolder(t)
	besideProgram := t.TempDir()
	if err := os.WriteFile(filepath.Join(besideProgram, "explorer.html"), []byte("beside the program"), 0o644); err != nil {
		t.Fatal(err)
	}
	programIsIn(t, besideProgram)

	page, err := explorerPage()
	if err != nil {
		t.Fatalf("the page beside the program was not found: %v", err)
	}
	if string(page) != "beside the program" {
		t.Errorf("read %q, want the page beside the program", page)
	}
}

func TestTheExplorerPageInTheFolderTheNodeWasStartedInComesFirst(t *testing.T) {
	here := startedInAnEmptyFolder(t)
	if err := os.WriteFile(filepath.Join(here, "explorer.html"), []byte("where it was started"), 0o644); err != nil {
		t.Fatal(err)
	}
	besideProgram := t.TempDir()
	if err := os.WriteFile(filepath.Join(besideProgram, "explorer.html"), []byte("beside the program"), 0o644); err != nil {
		t.Fatal(err)
	}
	programIsIn(t, besideProgram)

	page, err := explorerPage()
	if err != nil {
		t.Fatal(err)
	}
	if string(page) != "where it was started" {
		t.Errorf("read %q, want the page in the folder the node was started in", page)
	}
}

func TestWithNoExplorerPageAnywhereTheNodeSaysSo(t *testing.T) {
	startedInAnEmptyFolder(t)
	programIsIn(t, t.TempDir())
	if _, err := explorerPage(); err == nil {
		t.Error("a page was read although there is none")
	}
	programIsIn(t, "")
	if _, err := explorerPage(); err == nil {
		t.Error("a page was read although there is none and the program's folder is not known")
	}
}
