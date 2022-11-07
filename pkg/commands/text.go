package commands

import (
	"fmt"
	"github.com/dominikbraun/graph"
	"github.com/spf13/cobra"
	"github.com/wolfi-dev/dag/pkg/pkggraph"
	"io"
	"log"
	"os"
)

func cmdText() *cobra.Command {
	var dir, arch string
	var showDependents bool
	text := &cobra.Command{
		Use:   "text",
		Short: "Print a sorted list of downstream dependent packages",
		RunE: func(cmd *cobra.Command, args []string) error {
			if arch == "arm64" {
				arch = "aarch64"
			}

			g, err := pkggraph.New(os.DirFS(dir))
			if err != nil {
				return err
			}

			if len(args) == 0 {
				if showDependents {
					log.Print("warning: the 'show dependents' option has no effect without specifying one or more package names")
				}
			} else {
				// ensure all packages exist in the graph
				for _, arg := range args {
					if _, err := g.Graph.Vertex(arg); err == graph.ErrVertexNotFound {
						return fmt.Errorf("package %q not found in graph", arg)
					}
				}

				// determine if we're examining dependencies or dependents
				var subgraph *pkggraph.Graph
				if showDependents {
					leaves := args
					subgraph, err = g.SubgraphWithLeaves(leaves)
					if err != nil {
						return err
					}
				} else {
					roots := args
					subgraph, err = g.SubgraphWithRoots(roots)
					if err != nil {
						return err
					}
				}

				g = subgraph
			}

			err = text(*g, arch, os.Stdout)
			if err != nil {
				return err
			}

			return nil
		},
	}
	text.Flags().StringVarP(&dir, "dir", "d", ".", "directory to search for melange configs")
	text.Flags().StringVarP(&arch, "arch", "a", "x86_64", "architecture to build for")
	text.Flags().BoolVarP(&showDependents, "show-dependents", "D", false, "show packages that depend on these packages, instead of these packages' dependencies")
	return text
}

func text(g pkggraph.Graph, arch string, w io.Writer) error {
	all, err := g.Sorted()
	if err != nil {
		return err
	}

	for _, node := range all {
		target, err := g.MakeTarget(node, arch)
		if err != nil {
			return err
		}

		fmt.Fprintf(w, "%s\n", target)
	}

	return nil
}
