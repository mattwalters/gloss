#!/bin/sh
# localtier.sh <label> <start> <count> — file://-only tier: generate refs
# locally, push to control.git, measure no-op fetch. No GitHub involvement.
set -e
S=$(cd "$(dirname "$0")" && pwd)
L=$1; START=$2; COUNT=$3
cd "$S/seed"
"$S/genrefs" perobj "$START" "$COUNT" "$S/pool.txt" | git update-ref --stdin
git pack-refs --all
git for-each-ref 'refs/writ/**' | wc -l
git push -q "file://$S/control.git" 'refs/writ/*:refs/writ/*'
sh "$S/measure.sh" "$S/seed" "file://$S/control.git" "$L-file" "$S/out"
