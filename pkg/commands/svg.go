package commands

import (
	"fmt"
	"log"
	"sort"

	"github.com/goccy/go-graphviz"
	"github.com/spf13/cobra"
	"github.com/wolfi-dev/dag/pkg/graph"
)

func cmdSVG() *cobra.Command {
	var dir, out string
	svg := &cobra.Command{
		Use:   "svg",
		Short: "Generate a graphviz SVG",
		RunE: func(cmd *cobra.Command, args []string) error {
			g := graph.New()
			if err := g.Walk(dir); err != nil {
				return err
			}
			if err := g.Validate(); err != nil {
				return err
			}
			if len(args) == 0 {
				summarize(g)
				viz(g, out)
			} else {
				sg := graph.New()
				for _, node := range args {
					sg.Crawl(g, node)
				}
				summarize(sg)
				viz(sg, out)
			}

			return nil
		},
	}
	svg.Flags().StringVarP(&dir, "dir", "d", ".", "directory to search for melange configs")
	svg.Flags().StringVarP(&out, "out", "o", "dag.svg", "output file")
	return svg
}

func summarize(g graph.Graph) {
	log.Println("nodes:", len(g.Nodes))
	e := 0
	for node := range g.Nodes {
		e += len(g.Edges[node])
	}
	log.Println("edges:", e)
}

func viz(g graph.Graph, out string) (err error) {
	v := graphviz.New()
	gr, err := v.Graph()
	if err != nil {
		log.Fatalf("graphviz: %v", err)
	}
	defer func() {
		if cerr := gr.Close(); err != nil {
			err = cerr
		}
		v.Close()
	}()

	// Sort nodes for deterministic output.
	nodes := []string{}
	for node := range g.Nodes {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)

	for _, src := range nodes {
		srcn, err := gr.CreateNode(src)
		if err != nil {
			return fmt.Errorf("graphviz: %w", err)
		}

		// Sort nodes for deterministic output.
		dsts := g.Edges[src]
		sort.Strings(dsts)

		for _, dst := range dsts {
			dstn, err := gr.CreateNode(dst)
			if err != nil {
				return fmt.Errorf("graphviz: %w", err)
			}

			if _, err := gr.CreateEdge("e", srcn, dstn); err != nil {
				return fmt.Errorf("graphviz: %w", err)
			}
		}
	}

	return v.RenderFilename(gr, graphviz.SVG, out)
}
