package commands

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
	"github.com/wolfi-dev/dag/pkg/graph"
)

func cmdText() *cobra.Command {
	var dir, arch string
	text := &cobra.Command{
		Use:   "text",
		Short: "Print a sorted list of downstream dependent packages",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO(jason): Accept and translate --arch=arm64 -> aarch64, etc.

			g := graph.New()
			if err := g.Walk(dir); err != nil {
				return err
			}
			if err := g.Validate(); err != nil {
				return err
			}
			text(g, args, arch)
			return nil
		},
	}
	text.Flags().StringVarP(&dir, "dir", "d", ".", "directory to search for melange configs")
	text.Flags().StringVarP(&arch, "arch", "a", "x86_64", "architecture to build for")
	return text
}

func text(g graph.Graph, roots []string, arch string) {
	seen := make(map[string]struct{})

	var walk func(node string)
	walk = func(node string) {
		if _, ok := seen[node]; ok {
			return
		}
		seen[node] = struct{}{}
		edges := g.Edges[node]
		fmt.Println(g.Package(node).MakeTarget(arch))
		sort.Strings(edges) // sorted for determinism
		for _, dep := range edges {
			walk(dep)
		}
	}
	for _, root := range roots {
		walk(root)
	}
}
