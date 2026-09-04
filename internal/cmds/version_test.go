package cmds

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestVersionSurfacesInLockstep is the single-source-of-truth guard for the
// release version. The shipped version lives on three surfaces — the VERSION
// file, the cobra root command's Version field (the `driftledger --version`
// output, which cobra derives from this field), and the first `## [x.y.z]`
// heading of CHANGELOG.md — and a bump that touches only one ships silently
// out of sync (the v0.6.0 changelog documents a real prior "VERSION stale at
// 0.4.0" lag). This test asserts all three AGREE with each other AND equal
// the shipped target below, so:
//   - on a tag whose surfaces still carry the old version it FAILS (proving
//     the bump was not yet applied to every surface); and
//   - on a tag where one surface was bumped but another was forgotten it
//     FAILS (catching the single-surface drift before release).
//
// When bumping the version, update wantVersion below and bump all three
// surfaces together; the test then passes again.
func TestVersionSurfacesInLockstep(t *testing.T) {
	wantVersion := "0.8.0"

	repoRoot := repoRoot(t)

	versionBytes, err := os.ReadFile(filepath.Join(repoRoot, "VERSION"))
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	versionFile := strings.TrimSpace(string(versionBytes))

	cobraVersion := NewRootCmd().Version

	changelogHead, err := changelogHeadVersion(filepath.Join(repoRoot, "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("parse CHANGELOG head: %v", err)
	}

	// Guard against single-surface drift: all three must agree with each other.
	if versionFile != cobraVersion {
		t.Errorf("version drift: VERSION file = %q but driftledger --version (cobra Version) = %q", versionFile, cobraVersion)
	}
	if versionFile != changelogHead {
		t.Errorf("version drift: VERSION file = %q but CHANGELOG head = %q", versionFile, changelogHead)
	}

	// Guard against a forgotten bump: all three must carry the shipped target.
	if versionFile != wantVersion {
		t.Errorf("VERSION file = %q, want %q (bump all version surfaces together)", versionFile, wantVersion)
	}
	if cobraVersion != wantVersion {
		t.Errorf("driftledger --version = %q, want %q (bump the cobra root Version field at internal/cmds/commands.go)", cobraVersion, wantVersion)
	}
	if changelogHead != wantVersion {
		t.Errorf("CHANGELOG head = %q, want %q (prepend a ## [%s] entry to CHANGELOG.md)", changelogHead, wantVersion, wantVersion)
	}
}

// changelogHeadVersion extracts the bare version (e.g. "0.8.0") from the first
// `## [x.y.z] - <date>` heading in CHANGELOG.md, which is the head release entry.
var changelogHeadRe = regexp.MustCompile(`^##\s+\[([^\]]+)\]`)

func changelogHeadVersion(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if m := changelogHeadRe.FindStringSubmatch(line); m != nil {
			return m[1], nil
		}
	}
	return "", fmt.Errorf("changelog: no ## [version] heading found")
}

// repoRoot returns the repository root (two directories above this test file,
// which lives in internal/cmds), so the test reads the real VERSION and
// CHANGELOG.md regardless of the test binary's working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller: cannot locate test source file")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}
