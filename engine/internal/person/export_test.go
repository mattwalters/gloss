package person

// FoldValueGeneral exposes the general folding path — NFC, case fold, NFC —
// without FoldValue's ASCII shortcut, so TestFoldValueASCIIFastPath can check
// the shortcut against the algorithm it is standing in for rather than
// assuming the two agree.
func FoldValueGeneral(s string) string { return nfc(caseFold(nfc(s))) }

// NFC and CaseFold expose the individual steps so the sweeps can exercise
// composition on its own rather than only through the whole pipeline.
var (
	NFC      = nfc
	CaseFold = caseFold
)

// CaseFoldRaw is x/text's case folding with the Cherokee fixed points not
// corrected — the defect this package works around, kept reachable so
// TestDifferentialCatchesTheDefects can show the guard is load-bearing rather
// than decorative.
func CaseFoldRaw(s string) string { return foldCaser.String(s) }
