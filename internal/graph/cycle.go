package graph

// HasConceptCycle reports whether the concept→concept edges in g form a cycle.
// It runs a DFS with visited/in-stack colouring over all concept nodes and
// concept-labelled edges only (edges with a non-empty Label that connect two
// concept IDs). Testing-block edges (to/from qN/aN nodes) are ignored.
func HasConceptCycle(g *Graph) bool {
	// Build the set of all concept IDs.
	concepts := make(map[string]bool)
	for _, c := range g.PassedConcepts {
		concepts[c.ID] = true
	}
	for _, c := range g.UntestedConcepts {
		concepts[c.ID] = true
	}

	// Build adjacency list for concept→concept edges only.
	adj := make(map[string][]string, len(concepts))
	for _, e := range g.Edges {
		if concepts[e.From] && concepts[e.To] {
			adj[e.From] = append(adj[e.From], e.To)
		}
	}

	// DFS state: 0 = unvisited, 1 = in stack, 2 = done.
	color := make(map[string]int, len(concepts))

	var dfs func(id string) bool
	dfs = func(id string) bool {
		color[id] = 1
		for _, next := range adj[id] {
			if color[next] == 1 {
				return true // back edge → cycle
			}
			if color[next] == 0 {
				if dfs(next) {
					return true
				}
			}
		}
		color[id] = 2
		return false
	}

	for id := range concepts {
		if color[id] == 0 {
			if dfs(id) {
				return true
			}
		}
	}
	return false
}

// ConceptReaches reports whether there is a directed path from `from` to `to`
// through concept→concept edges. Returns false when either ID is not a concept
// in g, or when no such path exists.
func ConceptReaches(g *Graph, from, to string) bool {
	// Build adjacency list for concept→concept edges.
	concepts := make(map[string]bool)
	for _, c := range g.PassedConcepts {
		concepts[c.ID] = true
	}
	for _, c := range g.UntestedConcepts {
		concepts[c.ID] = true
	}
	if !concepts[from] || !concepts[to] {
		return false
	}

	adj := make(map[string][]string)
	for _, e := range g.Edges {
		if concepts[e.From] && concepts[e.To] {
			adj[e.From] = append(adj[e.From], e.To)
		}
	}

	visited := make(map[string]bool)
	var dfs func(id string) bool
	dfs = func(id string) bool {
		if id == to {
			return true
		}
		visited[id] = true
		for _, next := range adj[id] {
			if !visited[next] {
				if dfs(next) {
					return true
				}
			}
		}
		return false
	}
	return dfs(from)
}
