#!/bin/sh
# measure.sh <client-repo-dir> <url> <label> <outdir>
# Runs a no-op `git fetch 'refs/writ/*:refs/writ/*'` 3x (timed), then once
# more with GIT_TRACE_PACKET + GIT_TRACE_CURL_NO_DATA for byte accounting.
set -e
CLIENT=$1; URL=$2; LABEL=$3; OUT=$4
mkdir -p "$OUT"

now() { perl -MTime::HiRes=time -e 'printf "%.3f", time'; }

for i in 1 2 3; do
  t0=$(now)
  git -C "$CLIENT" -c protocol.version=2 fetch --no-write-fetch-head --quiet \
    "$URL" 'refs/writ/*:refs/writ/*'
  t1=$(now)
  printf '%s noop-fetch run%s %s\n' "$LABEL" "$i" \
    "$(awk -v a="$t0" -v b="$t1" 'BEGIN{printf "%.3f", b-a}')" \
    | tee -a "$OUT/times.txt"
done

rm -f "$OUT/$LABEL.pkt" "$OUT/$LABEL.curl"
t0=$(now)
GIT_TRACE_PACKET="$OUT/$LABEL.pkt" GIT_TRACE_CURL_NO_DATA=1 \
  GIT_TRACE_CURL="$OUT/$LABEL.curl" \
  git -C "$CLIENT" -c protocol.version=2 fetch --no-write-fetch-head --quiet \
  "$URL" 'refs/writ/*:refs/writ/*'
t1=$(now)
printf '%s noop-fetch traced %s\n' "$LABEL" \
  "$(awk -v a="$t0" -v b="$t1" 'BEGIN{printf "%.3f", b-a}')" \
  | tee -a "$OUT/times.txt"

# Packet-level accounting: server->client lines are "packet: fetch< ..."
awk '
  /packet:[ ]+(git|fetch)< / {
    payload = $0; sub(/.*(git|fetch)< /, "", payload)
    total += length(payload) + 5           # pkt-line: 4-byte len + payload + LF
    if (payload ~ /^[0-9a-f]{40,64} refs\//) { refs++; refbytes += length(payload) + 5 }
  }
  END { printf "recv_pkt_bytes=%d lsrefs_lines=%d lsrefs_bytes=%d\n", total, refs, refbytes }
' "$OUT/$LABEL.pkt" | tee -a "$OUT/times.txt"

# Curl-level accounting (headers + data sizes; body sizes are post-TLS,
# pre-decompression as seen by curl)
if [ -s "$OUT/$LABEL.curl" ]; then
  grep -E 'Recv (data|header)' "$OUT/$LABEL.curl" | awk '
    { n = 0; for (i = 1; i <= NF; i++) if ($i == "bytes") n = $(i-1) }
    /Recv header/ { h += n }
    /Recv data/   { d += n }
    END { printf "curl_recv_header_bytes=%d curl_recv_data_bytes=%d\n", h, d }
  ' | tee -a "$OUT/times.txt" || true
fi
