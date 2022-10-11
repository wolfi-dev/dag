package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-graphviz"
	"gopkg.in/yaml.v3"
)

var (
	// TODO: use cobra, have separate subcommands for svg vs text output.
	out = flag.String("f", "dag.svg", "output file")
	txt = flag.Bool("txt", false, "output newline-delimited sorted downstream deps, instead of generating SVG")
	dir = flag.String("d", ".", "directory to search for melange configs")
)

type Graph struct {
	Nodes       map[string]struct{}
	Edges, Back map[string][]string
}

func NewGraph() Graph {
	return Graph{
		Nodes: make(map[string]struct{}),
		Edges: make(map[string][]string),
		Back:  make(map[string][]string),
	}
}

func (g Graph) Contains(node string) bool {
	_, found := g.Nodes[node]
	return found
}

func (g Graph) Add(node string) {
	g.Nodes[node] = struct{}{}
}

func (g Graph) AddEdge(src, dst string) {
	if src == "" || dst == "" {
		log.Fatalf("empty %q -> %q", src, dst)
	}
	g.Nodes[src] = struct{}{}
	g.Nodes[dst] = struct{}{}
	g.Edges[src] = append(g.Edges[src], dst)
	g.Back[dst] = append(g.Back[dst], src)
}

type config struct {
	Package struct {
		Name string
	}
	Environment struct {
		Contents struct {
			Packages []string
		}
	}
	Subpackages []struct {
		Name string
	}
}

func main() {
	flag.Parse()

	g := NewGraph()
	if err := filepath.Walk(*dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Don't walk into directories.
		if path != *dir && info.IsDir() {
			return filepath.SkipDir
		}
		if strings.HasSuffix(path, ".yaml") {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			var c config
			if err := yaml.NewDecoder(f).Decode(&c); err != nil {
				return err
			}
			this := c.Package.Name
			if this == "" {
				log.Fatalf("no package name in %q", path)
			}
			for _, pkg := range c.Environment.Contents.Packages {
				if pkg == "" {
					log.Fatalf("empty package name in %q", path)
				}
				g.Add(pkg)
				g.AddEdge(this, pkg)
			}
			for _, subpkg := range c.Subpackages {
				g.Add(subpkg.Name)
				g.AddEdge(subpkg.Name, this)
			}
		}
		return nil

	}); err != nil {
		log.Fatalf("walk: %v", err)
	}

	// Validate all edges point to existing nodes.
	for from, to := range g.Edges {
		if !g.Contains(from) {
			log.Fatalf("%q", from)
		}
		for _, too := range to {
			if !g.Contains(too) {
				log.Fatalf("%q -> %q: %q not found", from, too, too)
			}
		}
	}

	if len(flag.Args()) == 0 {
		g.summarize()
		g.viz()
	} else {
		sg := NewGraph()
		for _, node := range flag.Args() {
			sg.crawl(g, node)
		}
		sg.summarize()
		if *txt {
			sg.text(flag.Args())
		} else {
			sg.viz()
		}
	}
}

func (g *Graph) text(roots []string) {
	seen := make(map[string]struct{})

	var walk func(node string)
	walk = func(node string) {
		if _, ok := seen[node]; ok {
			return
		}
		seen[node] = struct{}{}
		fmt.Println(node)
		edges := g.Edges[node]
		sort.Strings(edges) // sorted for determinism
		for _, dep := range edges {
			walk(dep)
		}
	}
	for _, root := range roots {
		walk(root)
	}
}

func (g Graph) summarize() {
	log.Println("nodes:", len(g.Nodes))
	e := 0
	for node := range g.Nodes {
		e += len(g.Edges[node])
	}
	log.Println("edges:", e)
}

// crawl crawls the given whole graph, starting at the given node, and adds nodes that depend on this node.
func (sg Graph) crawl(g Graph, node string) {
	g.Add(node)
	for _, dep := range g.Edges[node] {
		if !sg.Contains(dep) {
			sg.Add(dep)
			sg.AddEdge(node, dep)
			sg.crawl(g, dep)
		}
	}
}

func (g Graph) viz() {
	v := graphviz.New()
	gr, err := v.Graph()
	if err != nil {
		log.Fatalf("graphviz: %v", err)
	}
	defer func() {
		if err := gr.Close(); err != nil {
			log.Fatal(err)
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
			log.Fatalf("graphviz: %v", err)
		}

		// Sort nodes for deterministic output.
		dsts := g.Edges[src]
		sort.Strings(dsts)

		for _, dst := range dsts {
			dstn, err := gr.CreateNode(dst)
			if err != nil {
				log.Fatalf("graphviz: %v", err)
			}

			if _, err := gr.CreateEdge("e", srcn, dstn); err != nil {
				log.Fatal(err)
			}
		}
	}

	if err := v.RenderFilename(gr, graphviz.SVG, *out); err != nil {
		log.Fatal(err)
	}
}
