package pkg

import (
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"sort"
	"strings"

	"chainguard.dev/melange/pkg/build"
	"github.com/dominikbraun/graph"
)

// A Graph represents an interdependent set of Wolfi packages defined in one or more Melange configuration files.
type Graph struct {
	Graph    graph.Graph[string, string]
	configs  map[string]build.Configuration
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
func NewGraph(dirFS fs.FS, dirPath string) (*Graph, error) {
	g := newGraph()

	var packages []string
	configs := make(map[string]build.Configuration)

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

			p := filepath.Join(dirPath, path)
			c, err := build.ParseConfiguration(p)
			if err != nil {
				return err
			}

			name := c.Package.Name
			if name == "" {
				log.Printf("no package name in %q", path)
				return nil
			}
			if p, exists := configs[name]; exists && !strings.HasPrefix(p.Package.Description, "PROVIDED BY") {
				log.Fatalf("duplicate package config found for %q in %q", c.Package.Name, path)
			}

			configs[name] = *c
			packages = append(packages, name)

			for _, prov := range c.Package.Dependencies.Provides {
				p, v, ok := strings.Cut(prov, "=")
				if !ok {
					log.Fatalf("don't know how to interpret %q in %s", prov, path)
				}
				if _, exists := configs[p]; !exists {
					configs[p] = build.Configuration{
						Package: build.Package{
							Name:        p,
							Version:     v,
							Description: fmt.Sprintf("PROVIDED BY %s", c.Package.Name),
						},
					}
				}
			}

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
				return nil, fmt.Errorf("unable to add vertex for %q subpackage %q: %w", name, subpkg.Name, err)
			}
			if err := g.AddEdge(subpkg.Name, c.Package.Name); err != nil {
				return nil, fmt.Errorf("unable to add edge for %q subpackage %q: %w", name, subpkg.Name, err)
			}

			// TODO: resolve deps via `uses` for subpackage pipelines.
		}

		// Resolve all `uses` used by the pipeline. This updates the set of
		// .environment.contents.packages so the next block can include those as build deps.
		pctx := &build.PipelineContext{
			Context: &build.Context{
				Configuration: c,
			},
			Package: &c.Package,
		}
		for _, s := range c.Pipeline {
			if err := s.ApplyNeeds(pctx); err != nil {
				return nil, fmt.Errorf("unable to resolve needs for package %s: %w", name, err)
			}
			c.Environment.Contents.Packages = pctx.Context.Configuration.Environment.Contents.Packages
		}

		for _, buildDep := range c.Environment.Contents.Packages {
			if buildDep == "" {
				log.Fatalf("empty package name in environment packages for %q", c.Package.Name)
			}
			if err := g.AddVertex(buildDep); err != nil {
				return nil, fmt.Errorf("unable to add vertex for %q dependency %q: %w", name, buildDep, err)
			}
			if err := g.AddEdge(c.Package.Name, buildDep); err != nil {
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

func isErrCycle(err error) bool {
	// TODO: suggest to the upstream graph lib that this be detectable via errors.As
	return strings.Contains(err.Error(), "would introduce a cycle")
}

// Config returns the Melange configuration for the package with the given name,
// if the package is present in the Graph. If it's not present, Config returns
// nil. Providing the name of a subpackage will return the configuration of the
// subpackage's origin package.
func (g Graph) Config(name string) *build.Configuration {
	if g.configs == nil {
		// this would be unexpected
		return nil
	}

	if c, ok := g.configs[name]; ok {
		return &c
	}

	return nil
}

// Sorted returns a list of all package names in the Graph, sorted in topological
// order, meaning that packages earlier in the list depend on packages later in
// the list.
func (g Graph) Sorted() ([]string, error) {
	return graph.TopologicalSort(g.Graph)
}

// SubgraphWithRoots returns a new Graph that's a subgraph of g, where the set of
// the new Graph's roots will be identical to or a subset of the given set of
// roots.
//
// In other words, the new subgraph will contain all dependencies (transitively)
// of all packages whose names were given as the `roots` argument.
func (g Graph) SubgraphWithRoots(roots []string) (*Graph, error) {
	subgraph := newGraph()
	configs := make(map[string]build.Configuration)
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
	configs := make(map[string]build.Configuration)
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
	if pkgName != config.Package.Name {
		return "", nil
	}

	p := config.Package

	// note: using pkgName here because it may be a subpackage, not the main package declared within the config (i.e. `p.Name`)
	return fmt.Sprintf("make packages/%s/%s-%s-r%d.apk", arch, pkgName, p.Version, p.Epoch), nil
}

func (g Graph) MakefileEntry(pkgName string) (string, error) {
	config := g.Config(pkgName)
	if config == nil {
		return "", fmt.Errorf("unable to generate target: no config for package %q", pkgName)
	}
	if pkgName != config.Package.Name {
		return "", nil
	}
	return fmt.Sprintf("$(eval $(call build-package,%s,%s-%d))", pkgName, config.Package.Version, config.Package.Epoch), nil
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
