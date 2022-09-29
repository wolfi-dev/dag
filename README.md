# `dag`

[![ci](https://github.com/wolfi-dev/dag/actions/workflows/build.yaml/badge.svg)](https://github.com/wolfi-dev/dag/actions/workflows/build.yaml)

`dag` generates Graphviz digraphs for Melange package dependencies.

To generate the full `dag.svg` of all build-time dependencies, run this inside the root of the [Wolfi OS](https://github.com/wolfi-dev/os) repo:

```
go run ./
```

![full example dependency graph](./images/dag.svg)

## Subgraphs

`dag` can also generate subgraphs for only some packages.

To generate a graph for only one package:

```
go run ./ brotli
```

To generate a graph for only some packages:

```
go run ./ brotli git-lfs attr
```

![partial dependency graph](./images/sub.svg)

### Output

`dag` writes a file called `dag.svg` by default.

To change this, pass `-f` _before any positional args_.

```
go run ./ -f brotli.svg brotli
```

It will only generate SVG.

