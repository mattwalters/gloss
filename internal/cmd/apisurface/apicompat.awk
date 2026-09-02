# apicompat.awk — keep the engine lines of an apidiff report.
#
# Run by `make api-compat`, which diffs the whole module against the merge
# base. The report therefore covers cmd/ and spec/ too; this keeps only the
# changes to engine and its subpackages, under the section header they arrived
# with. Lives in a file rather than in the recipe so it can be tested — see
# apicompat_test.go.
#
# Two spellings have to match, because apidiff formats them differently. A
# symbol is named relative to the module root:
#
#	- ./engine/resolve.SHA256: value changed from 1 to 2
#
# while a package added or removed as a whole is named by its full import path
# (apidiff's packageChange):
#
#	- package example.com/m/engine/labels: removed
#
# Matching only the first spelling would let a deleted public subpackage — the
# most breaking change short of deleting the module — pass through the report
# unclassified, which is the blind spot this report exists to close.
#
# Each section is sorted, because apidiff walks a Go map and its own line order
# varies between runs. Incompatible changes come first. Invoke under LC_ALL=C
# so the collation cannot vary either.
#
# Usage: awk -f apicompat.awk -v mod=example.com/m report.txt

BEGIN {
	if (mod == "") {
		print "apicompat.awk: -v mod=MODULEPATH is required" > "/dev/stderr"
		exit 2
	}
	esc = mod
	gsub(/\./, "\\.", esc)
	pkgre = "^- package " esc "/engine([/.]|: )"
}

# The only lines ending in ":" that are not changes are the two section
# headers apidiff prints, "Incompatible changes:" and "Compatible changes:".
/:$/ && !/^-/ {
	header = $0
	rank = (header ~ /^Incompatible/) ? 0 : 1
	next
}

header != "" && ($0 ~ /\.\/engine/ || $0 ~ pkgre) {
	n[rank]++
	line[rank, n[rank]] = $0
	head[rank] = header
}

END {
	for (r = 0; r <= 1; r++) {
		if (!(r in n)) {
			continue
		}
		# Insertion sort: awk has no portable sort, and these lists are
		# a handful of lines long.
		for (i = 2; i <= n[r]; i++) {
			v = line[r, i]
			for (j = i - 1; j >= 1 && line[r, j] > v; j--) {
				line[r, j + 1] = line[r, j]
			}
			line[r, j + 1] = v
		}
		print head[r]
		for (i = 1; i <= n[r]; i++) {
			print line[r, i]
		}
	}
}
