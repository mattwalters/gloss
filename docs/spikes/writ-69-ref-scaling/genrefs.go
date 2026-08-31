// genrefs emits "create <ref> <sha>" lines for `git update-ref --stdin`.
//
// Modes:
//
//	genrefs perobj <start> <count> <poolfile>
//	    refs/writ/w<8hex>/cobs/<type>/<40hex> — one ref per (writer, object).
//	    Writer is object-index mod 50; object id is sha256-derived, so ref
//	    names have realistic entropy (matters if the wire path compresses).
//
//	genrefs chain <writers> <poolfile>
//	    refs/writ/w<8hex>/<type> — one chain ref per writer per type.
//	    4 types => writers*4 refs.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
)

var types = []string{"review", "comment", "approval", "thread"}

func main() {
	if len(os.Args) < 2 {
		die("usage: genrefs perobj <start> <count> <poolfile> | genrefs chain <writers> <poolfile>")
	}
	mode := os.Args[1]
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	switch mode {
	case "perobj":
		start := atoi(os.Args[2])
		count := atoi(os.Args[3])
		pool := readPool(os.Args[4])
		for i := start; i < start+count; i++ {
			writer := hexOf(fmt.Sprintf("writer:%d", i%50))[:8]
			objID := hexOf(fmt.Sprintf("writ69:obj:%d", i))[:40]
			typ := types[i%len(types)]
			fmt.Fprintf(out, "create refs/writ/w%s/cobs/%s/%s %s\n",
				writer, typ, objID, pool[i%len(pool)])
		}
	case "chain":
		writers := atoi(os.Args[2])
		pool := readPool(os.Args[3])
		n := 0
		for w := 0; w < writers; w++ {
			writer := hexOf(fmt.Sprintf("writer:%d", w))[:8]
			for _, typ := range types {
				fmt.Fprintf(out, "create refs/writ/w%s/%s %s\n",
					writer, typ, pool[n%len(pool)])
				n++
			}
		}
	default:
		die("unknown mode: " + mode)
	}
}

func hexOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func readPool(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		die(err.Error())
	}
	lines := strings.Fields(string(b))
	if len(lines) == 0 {
		die("empty pool")
	}
	return lines
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		die("bad int: " + s)
	}
	return n
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
