//go:build probe

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func main() {
	gapsPath := ""
	condstore := false
	listProbe := false
	args := os.Args[1:]
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "-gaps":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "usage: cyrusprobe [-gaps gaps.json] [-condstore] [-list] operator.json")
				os.Exit(2)
			}
			gapsPath = args[1]
			args = args[2:]
		case "-condstore":
			condstore = true
			args = args[1:]
		case "-list":
			listProbe = true
			args = args[1:]
		default:
			fmt.Fprintln(os.Stderr, "usage: cyrusprobe [-gaps gaps.json] [-condstore] [-list] operator.json")
			os.Exit(2)
		}
	}
	if condstore && listProbe {
		fmt.Fprintln(os.Stderr, "usage: cyrusprobe [-gaps gaps.json] [-condstore] [-list] operator.json")
		os.Exit(2)
	}
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: cyrusprobe [-gaps gaps.json] [-condstore] [-list] operator.json")
		os.Exit(2)
	}
	cfg, err := loadOpConfig(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if listProbe {
		diff, err := compareList(cfg)
		if diff != "" {
			fmt.Fprint(os.Stdout, diff)
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stdout, "diff empty")
		return
	}
	if condstore {
		diff, err := compareCondstore(cfg)
		if diff != "" {
			fmt.Fprint(os.Stdout, diff)
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stdout, "diff empty")
		return
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
