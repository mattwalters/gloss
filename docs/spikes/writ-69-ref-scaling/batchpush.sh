#!/bin/sh
# batchpush.sh <label> <chunk> — push local refs/writ/* missing from origin,
# <chunk> ref updates per push. Logs per-chunk wall time.
set -e
S=$(cd "$(dirname "$0")" && pwd)
L=$1; CHUNK=$2
cd "$S/seed"
now() { perl -MTime::HiRes=time -e 'printf "%.3f", time'; }

git ls-remote origin 'refs/writ/*' | awk '{print $2}' | sort > "$S/out/$L.remote"
git for-each-ref --format='%(refname)' 'refs/writ/**' | sort > "$S/out/$L.local"
comm -23 "$S/out/$L.local" "$S/out/$L.remote" > "$S/out/$L.missing"
total=$(wc -l < "$S/out/$L.missing" | tr -d ' ')
echo "$L missing=$total chunk=$CHUNK"

split -l "$CHUNK" "$S/out/$L.missing" "$S/out/$L.chunk."
tstart=$(now)
for f in "$S/out/$L.chunk."*; do
  t0=$(now)
  sed 's/\(.*\)/\1:\1/' "$f" | xargs git push -q origin 2>"$S/out/$L-push.err" || {
    echo "$L chunk $f FAILED"; tail -4 "$S/out/$L-push.err"; exit 1; }
  t1=$(now)
  awk -v a=$t0 -v b=$t1 -v n="$(wc -l < "$f" | tr -d ' ')" -v l=$L \
    'BEGIN{printf "%s push-chunk %d refs %.3fs\n", l, n, b-a}' | tee -a "$S/out/times.txt"
done
tend=$(now)
awk -v a=$tstart -v b=$tend -v l=$L -v t=$total \
  'BEGIN{printf "%s push-increment-total %d refs %.3fs\n", l, t, b-a}' | tee -a "$S/out/times.txt"
rm -f "$S/out/$L.chunk."*
