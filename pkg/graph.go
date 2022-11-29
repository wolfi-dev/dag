package pkg

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"sort"
	"strings"

	"github.com/dominikbraun/graph"
	"gopkg.in/yaml.v3"
)

// A Graph represents an interdependent set of Wolfi packages defined in one or more Melange configuration files.
type Graph struct {
	Graph    graph.Graph[string, string]
	configs  map[string]Config
	packages []string
}

func newGraph() graph.Graph[string, string] {
	return graph.New(graph.StringHash, graph.Directed(), graph.Acyclic(), graph.PreventCycles())
}

// NewGraph returns a new Graph using Melange configuration discovered in the given directory.
//
// The input is any fs.FS filesystem implementation. Given a directory path, you can call NewGraph like this:
//
// pkg.NewGraph(os.DirFS('path/to/directory'))
func NewGraph(dirFS fs.FS) (*Graph, error) {
	g := newGraph()

	var packages []string
	configs := make(map[string]Config)

	err := fs.WalkDir(dirFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() && path != "." {
			return fs.SkipDir
		}

		if d.Type().IsRegular() && strings.HasSuffix(path, ".yaml") {
			f, err := dirFS.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()

			c, err := decodeMelangeYAML(f)
			if err != nil {
				return err
			}
			name := c.Package.Name
			if name == "" {
				log.Fatalf("no package name in %q", path)
			}
			if _, exists := configs[name]; exists {
				log.Fatalf("duplicate package config found for %q in %q", c.Package, path)
			}

			configs[name] = c
			packages = append(packages, name)

			g.AddVertex(name)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, name := range packages {
		c := configs[name]

		for _, subpkg := range c.Subpackages {
			if subpkg.Name == "" {
				log.Fatalf("empty subpackage name for %q", c.Package.Name)
			}

			if _, exists := configs[subpkg.Name]; exists {
				log.Fatalf("subpackage name %q (listed in package %q) was used already", subpkg.Name, c.Package.Name)
			}
			configs[subpkg.Name] = c
			err := g.AddVertex(subpkg.Name)
			if err != nil {
				return nil, fmt.Errorf("unable to add vertex for %q subpackage %q: %w", name, subpkg, err)
			}
			err = g.AddEdge(subpkg.Name, c.Package.Name)
			if err != nil {
				return nil, fmt.Errorf("unable to add edge for %q subpackage %q: %w", name, subpkg, err)
			}
		}

		for _, buildDep := range c.Environment.Contents.Packages {
			if buildDep == "" {
				log.Fatalf("empty package name in environment packages for %q", c.Package.Name)
			}
			err = g.AddVertex(buildDep)
			if err != nil {
				return nil, fmt.Errorf("unable to add vertex for %q dependency %q: %w", name, buildDep, err)
			}
			err = g.AddEdge(c.Package.Name, buildDep)
			if err != nil {
				if isErrCycle(err) {
					log.Printf("warning: package %q depedendency on %q would introduce a cycle, so %q needs to be provided via bootstrapping", name, buildDep, buildDep)
				} else {
					return nil, fmt.Errorf("unable to add edge for %q dependency %q: %w", name, buildDep, err)
				}
			}
		}
	}

	return &Graph{
		Graph:    g,
		configs:  configs,
		packages: packages,
	}, nil
}

func decodeMelangeYAML(f fs.File) (Config, error) {
	stat, err := f.Stat()
	if err != nil {
		return Config{}, err
	}

	b, err := io.ReadAll(f)
	if err != nil {
		return Config{}, fmt.Errorf("unable to decode %q: %w", stat.Name(), err)
	}

	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("unable to decode %q: %w", stat.Name(), err)
	}

	// Hydrate subpackages that use a range.
	var updated []Subpackage
	for _, sp := range c.Subpackages {
		if sp.Range == "" {
			updated = append(updated, Subpackage{Name: sp.Name})
		} else {
			for _, d := range c.Data {
				if d.Name == sp.Range {
					for k, v := range d.Items {
						n := d.Name
						n = strings.ReplaceAll(n, "${{range.key}}", k)
						n = strings.ReplaceAll(n, "${{range.value}}", v)
						updated = append(updated, Subpackage{Name: n})
					}
					break
				}
			}
		}
	}
	sort.Slice(updated, func(i, j int) bool { return updated[i].Name < updated[j].Name })
	c.Subpackages = updated

	return c, nil
}

func isErrCycle(err error) bool {
	// TODO: suggest to the upstream graph lib that this be detectable via errors.As
	return strings.Contains(err.Error(), "would introduce a cycle")
}

// Config returns the Melange configuration for the package with the given name,
// if the package is present in the Graph. If it's not present, Config returns
// nil. Providing the name of a subpackage will return the configuration of the
// subpackage's origin package.
func (g Graph) Config(name string) *Config {
	if g.configs == nil {
		// this would be unexpected
		return nil
	}

	if c, ok := g.configs[name]; ok {
		return &c
	}

	return nil
}

// IsSubpackage returns a bool indicating whether the package with the given name
// is a subpackage. If the package is an origin package, or if the package is not
// found in the graph, IsSubpackage returns false.
func (g Graph) IsSubpackage(name string) bool {
	c := g.Config(name)

	if c == nil {
		// This (sub)package doesn't exist in the graph.
		return false
	}

	return c.Package.Name != name
}

// Sorted returns a list of all package names in the Graph, sorted in topological
// order, meaning that packages earlier in the list depend on packages later in
// the list.
func (g Graph) Sorted() ([]string, error) {
	sorted, err := graph.TopologicalSort(g.Graph)
	if err != nil {
		return nil, err
	}

	return sorted, nil
}

// SubgraphWithRoots returns a new Graph that's a subgraph of g, where the set of
// the new Graph's roots will be identical to or a subset of the given set of
// roots.
//
// In other words, the new subgraph will contain all dependencies (transitively)
// of all packages whose names were given as the `roots` argument.
func (g Graph) SubgraphWithRoots(roots []string) (*Graph, error) {
	subgraph := newGraph()
	configs := make(map[string]Config)
	var packages []string

	adjacencyMap, err := g.Graph.AdjacencyMap()
	if err != nil {
		return nil, err
	}

	var walk func(key string) // Go can be so awkward sometimes!
	walk = func(key string) {
		subgraph.AddVertex(key)
		packages = append(packages, key)

		c := g.Config(key)
		if c != nil {
			configs[key] = *c
		}

		for dependency := range adjacencyMap[key] {
			subgraph.AddVertex(dependency)
			subgraph.AddEdge(key, dependency)

			walk(dependency)
		}
	}

	for _, root := range roots {
		walk(root)
	}

	return &Graph{
		Graph:    subgraph,
		configs:  configs,
		packages: packages,
	}, nil
}

// SubgraphWithLeaves returns a new Graph that's a subgraph of g, where the set of
// the new Graph's leaves will be identical to or a subset of the given set of
// leaves.
//
// In other words, the new subgraph will contain all packages (transitively) that
// are dependent on the packages whose names were given as the `leaves` argument.
func (g Graph) SubgraphWithLeaves(leaves []string) (*Graph, error) {
	subgraph := newGraph()
	configs := make(map[string]Config)
	var packages []string

	predecessorMap, err := g.Graph.PredecessorMap()
	if err != nil {
		return nil, err
	}

	var walk func(key string) // Go can be so awkward sometimes!
	walk = func(key string) {
		subgraph.AddVertex(key)
		packages = append(packages, key)

		c := g.Config(key)
		if c != nil {
			configs[key] = *c
		}

		for dependent := range predecessorMap[key] {
			subgraph.AddVertex(dependent)
			subgraph.AddEdge(dependent, key)

			walk(dependent)
		}
	}

	for _, leaf := range leaves {
		walk(leaf)
	}

	return &Graph{
		Graph:    subgraph,
		configs:  configs,
		packages: packages,
	}, nil
}

// MakeTarget creates the make target for the given package in the Graph.
func (g Graph) MakeTarget(pkgName, arch string) (string, error) {
	config := g.Config(pkgName)
	if config == nil {
		return "", fmt.Errorf("unable to generate target: no config for package %q", pkgName)
	}

	p := config.Package

	// note: using pkgName here because it may be a subpackage, not the main package declared within the config (i.e. `p.Name`)
	return fmt.Sprintf("packages/%s/%s-%s-r%s.apk", arch, pkgName, p.Version, p.Epoch), nil
}

// Nodes returns a slice of the names of all nodes in the Graph, sorted alphabetically.
func (g Graph) Nodes() []string {
	allPackages := g.packages

	// sort for deterministic output
	sort.Strings(allPackages)
	return allPackages
}

// DependenciesOf returns a slice of the names of the given package's dependencies, sorted alphabetically.
func (g Graph) DependenciesOf(node string) []string {
	adjacencyMap, err := g.Graph.AdjacencyMap()
	if err != nil {
		return nil
	}

	var dependencies []string

	if deps, ok := adjacencyMap[node]; ok {
		for dep := range deps {
			dependencies = append(dependencies, dep)
		}

		// sort for deterministic output
		sort.Strings(dependencies)
		return dependencies
	}

	return nil
}
