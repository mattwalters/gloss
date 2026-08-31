#!/bin/sh
# tier.sh <label> — push refs/writ/* increment, sync control, measure GH +
# file:// no-op fetches, then fresh clone + first fetch. Assumes local refs
# for the tier already exist in ./seed.
set -e
S=$(cd "$(dirname "$0")" && pwd)
L=$1
now() { perl -MTime::HiRes=time -e 'printf "%.3f", time'; }

cd "$S/seed"
t0=$(now); git push -q origin 'refs/writ/*:refs/writ/*' 2>"$S/out/$L-push.err"; t1=$(now)
awk -v a=$t0 -v b=$t1 -v l=$L 'BEGIN{printf "%s push-increment %.3f\n", l, b-a}' | tee -a "$S/out/times.txt"
git push -q "file://$S/control.git" 'refs/writ/*:refs/writ/*'

sh "$S/measure.sh" "$S/seed" https://github.com/mattwalters/writ-69-ref-scaling-spike.git "$L-gh" "$S/out"
sh "$S/measure.sh" "$S/seed" "file://$S/control.git" "$L-file" "$S/out"

rm -rf "$S/fresh"
t0=$(now)
git -c credential.helper='!gh auth git-credential' clone -q https://github.com/mattwalters/writ-69-ref-scaling-spike.git "$S/fresh"
t1=$(now)
git -C "$S/fresh" config credential.helper '!gh auth git-credential'
t2=$(now); git -C "$S/fresh" fetch -q origin 'refs/writ/*:refs/writ/*'; t3=$(now)
awk -v a=$t0 -v b=$t1 -v c=$t2 -v d=$t3 -v l=$L 'BEGIN{printf "%s clone %.3f first-writ-fetch %.3f\n", l, b-a, d-c}' | tee -a "$S/out/times.txt"
git -C "$S/fresh" for-each-ref 'refs/writ/**' | wc -l
