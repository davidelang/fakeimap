//go:build probe

package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	gapsPath := ""
	args := os.Args[1:]
	if len(args) >= 2 && args[0] == "-gaps" {
		gapsPath = args[1]
		args = args[2:]
	}
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: cyrusprobe [-gaps gaps.json] operator.json")
		os.Exit(2)
	}
	cfg, err := loadOpConfig(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	diff, gaps, err := compareProbe(cfg)
	if gapsPath != "" {
		b, merr := json.MarshalIndent(gaps, "", "  ")
		if merr != nil {
			fmt.Fprintln(os.Stderr, merr)
			os.Exit(1)
		}
		b = append(b, '\n')
		if werr := os.WriteFile(gapsPath, b, 0644); werr != nil {
			fmt.Fprintln(os.Stderr, werr)
			os.Exit(1)
		}
	}
	if diff != "" {
		fmt.Fprint(os.Stdout, diff)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, "diff empty")
}
