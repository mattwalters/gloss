package main

import (
	"fmt"
	"io"
)

func runHelp(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		renderHelp(stdout, nil, rootCommand)
		return 0
	}

	cmd, path, err := findCommandByPath(args)
	if err != nil {
		fmt.Fprintf(stderr, "writ help: %v\n", err)
		return 2
	}

	renderHelp(stdout, path, cmd)
	return 0
}

func renderHelp(w io.Writer, path []string, c *command) {
	renderUsage(w, path, c)
	if len(c.Examples) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Examples:")
		for _, ex := range c.Examples {
			fmt.Fprintf(w, "  %s\n", ex)
		}
	}
}
