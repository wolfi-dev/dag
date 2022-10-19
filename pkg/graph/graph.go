package graph

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// TODO(jason): Add a method to emit roots (nodes that have no incoming edges).

type Graph struct {
	Nodes       map[string]struct{}
	Edges, Back map[string][]string
	Packages    map[string]Package
}

type Package struct {
	name, version, epoch string
}

func (p *Package) MakeTarget(arch string) string {
	if p.version == "" {
		return fmt.Sprintf("package %q not found!", p.name)
	}
	return fmt.Sprintf("packages/%s/%s-%s-r%s.apk", arch, p.name, p.version, p.epoch)
}

func New() Graph {
	return Graph{
		Nodes:    make(map[string]struct{}),
		Edges:    make(map[string][]string),
		Back:     make(map[string][]string),
		Packages: make(map[string]Package),
	}
}

func (g Graph) Walk(dir string) error {
	return filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Don't walk into directories.
		if path != dir && info.IsDir() {
			return filepath.SkipDir
		}
		if strings.HasSuffix(path, ".yaml") {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			var c struct {
				Package struct {
					Name    string `yaml:"name"`
					Version string `yaml:"version"`
					Epoch   string `yaml:"epoch"`
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
				g.AddPackage(subpkg.Name, c.Package.Version, c.Package.Epoch)
			}
			g.AddPackage(this, c.Package.Version, c.Package.Epoch)
		}
		return nil
	})
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

func (g Graph) Package(p string) *Package {
	if pk, ok := g.Packages[p]; ok {
		return &pk
	}
	return &Package{name: p}
}

func (g Graph) AddPackage(p, version, epoch string) {
	g.Packages[p] = Package{
		name:    p,
		version: version,
		epoch:   epoch,
	}
}

// Crawl crawls the given whole graph, starting at the given node, and adds nodes that depend on this node.
func (sg Graph) Crawl(g Graph, node string) {
	g.Add(node)
	sg.Packages[node] = g.Packages[node]
	for _, dep := range g.Edges[node] {
		if !sg.Contains(dep) {
			sg.Add(dep)
			sg.AddEdge(node, dep)
			sg.Packages[dep] = g.Packages[dep]
			sg.Crawl(g, dep)
		}
	}
}

// Validate validates that all edges point to existing nodes.
func (g Graph) Validate() error {
	for from, to := range g.Edges {
		if !g.Contains(from) {
			return fmt.Errorf("%q -> %q: %q not found", from, to, from)
		}
		for _, too := range to {
			if !g.Contains(too) {
				return fmt.Errorf("%q -> %q: %q not found", from, too, too)
			}
		}
	}
	return nil
}
