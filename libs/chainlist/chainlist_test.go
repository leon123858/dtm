package chainlist

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func pointer[T any](value T) *T { return &value }

func collectWalk[K comparable, V any](seq func(func(Node[K, V], error) bool)) ([]Node[K, V], error) {
	var nodes []Node[K, V]
	for node, err := range seq {
		if err != nil {
			return nodes, err
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func ids(nodes []Node[int, string]) []int {
	result := make([]int, len(nodes))
	for i, node := range nodes {
		result[i] = node.ID
	}
	return result
}

func TestTailFromLoadsOnlyForwardChain(t *testing.T) {
	nodes := map[int]Node[int, string]{
		1: {ID: 1, ChildID: pointer(2)},
		2: {ID: 2, ParentID: pointer(1), ChildID: pointer(3)},
		3: {ID: 3, ParentID: pointer(2)},
		9: {ID: 9},
	}
	var loaded []int
	loader := LoaderFunc[int, string](func(_ context.Context, id int) (Node[int, string], error) {
		loaded = append(loaded, id)
		node, ok := nodes[id]
		if !ok {
			return Node[int, string]{}, ErrNodeNotFound
		}
		return node, nil
	})

	tail, err := TailFrom(context.Background(), loader, 2)
	if err != nil {
		t.Fatal(err)
	}
	if tail.ID != 3 {
		t.Fatalf("tail ID = %d, want 3", tail.ID)
	}
	if !slices.Equal(loaded, []int{2, 3}) {
		t.Fatalf("loaded %v, want [2 3]", loaded)
	}
}

func TestMemoryAndLazySourcesImplementChainList(t *testing.T) {
	nodes := []Node[int, string]{
		{ID: 3, ParentID: pointer(2), Value: "tail"},
		{ID: 1, ChildID: pointer(2), Value: "root"},
		{ID: 2, ParentID: pointer(1), ChildID: pointer(3), Value: "middle"},
	}
	memory, err := NewMemorySource(nodes)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[int]Node[int, string], len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	lazy, err := NewLazySource(LoaderFunc[int, string](func(_ context.Context, id int) (Node[int, string], error) {
		node, ok := byID[id]
		if !ok {
			return Node[int, string]{}, ErrNodeNotFound
		}
		return node, nil
	}), []int{1})
	if err != nil {
		t.Fatal(err)
	}

	implementations := []struct {
		name string
		list ChainList[int, string]
	}{
		{name: "memory", list: memory},
		{name: "lazy", list: lazy},
	}
	for _, implementation := range implementations {
		t.Run(implementation.name, func(t *testing.T) {
			tail, tailErr := implementation.list.TailFrom(context.Background(), 2)
			if tailErr != nil {
				t.Fatal(tailErr)
			}
			if tail.ID != 3 {
				t.Fatalf("tail ID = %d, want 3", tail.ID)
			}

			chain, chainErr := collectWalk(implementation.list.WalkCanonical(context.Background(), 2))
			if chainErr != nil {
				t.Fatal(chainErr)
			}
			if !slices.Equal(ids(chain), []int{1, 2, 3}) {
				t.Fatalf("canonical IDs = %v, want [1 2 3]", ids(chain))
			}

			var chains [][]Node[int, string]
			for complete, completeErr := range implementation.list.Chains(
				context.Background(),
				func(a, b Node[int, string]) int { return a.ID - b.ID },
			) {
				if completeErr != nil {
					t.Fatal(completeErr)
				}
				chains = append(chains, complete)
			}
			if len(chains) != 1 || !slices.Equal(ids(chains[0]), []int{1, 2, 3}) {
				t.Fatalf("chains = %v, want [[1 2 3]]", chains)
			}
		})
	}
}

func TestLazySourceDefersNodeLoadingUntilIteration(t *testing.T) {
	loaded := 0
	lazy, err := NewLazySource(LoaderFunc[int, string](func(_ context.Context, id int) (Node[int, string], error) {
		loaded++
		return Node[int, string]{ID: id}, nil
	}), []int{1})
	if err != nil {
		t.Fatal(err)
	}

	walk := lazy.WalkFrom(context.Background(), 1)
	if loaded != 0 {
		t.Fatalf("loader called %d times before iteration", loaded)
	}
	for node, walkErr := range walk {
		if walkErr != nil {
			t.Fatal(walkErr)
		}
		if node.ID != 1 {
			t.Fatalf("node ID = %d, want 1", node.ID)
		}
	}
	if loaded != 1 {
		t.Fatalf("loader called %d times, want 1", loaded)
	}
}

func TestNewLazySourceRejectsInvalidConfiguration(t *testing.T) {
	_, err := NewLazySource[int, string](nil, nil)
	if !errors.Is(err, ErrNilLoader) {
		t.Fatalf("nil loader error = %v, want ErrNilLoader", err)
	}
	loader := LoaderFunc[int, string](func(_ context.Context, id int) (Node[int, string], error) {
		return Node[int, string]{ID: id}, nil
	})
	_, err = NewLazySource(loader, []int{1, 1})
	if !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("duplicate root error = %v, want ErrDuplicateID", err)
	}
}

func TestWalkCanonicalReturnsRootToTail(t *testing.T) {
	source, err := NewMemorySource([]Node[int, string]{
		{ID: 3, ParentID: pointer(2), Value: "tail"},
		{ID: 1, ChildID: pointer(2), Value: "root"},
		{ID: 2, ParentID: pointer(1), ChildID: pointer(3), Value: "middle"},
	})
	if err != nil {
		t.Fatal(err)
	}

	chain, err := collectWalk(WalkCanonical(context.Background(), source, 2))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids(chain), []int{1, 2, 3}) {
		t.Fatalf("chain IDs = %v, want [1 2 3]", ids(chain))
	}
}

func TestNonCanonicalBranchIsIgnored(t *testing.T) {
	source, err := NewMemorySource([]Node[int, string]{
		{ID: 1, ChildID: pointer(2)},
		{ID: 2, ParentID: pointer(1)},
		{ID: 3, ParentID: pointer(1), ChildID: pointer(4)},
		{ID: 4, ParentID: pointer(3)},
	})
	if err != nil {
		t.Fatal(err)
	}

	var chains [][]Node[int, string]
	for chain, chainErr := range source.Chains(context.Background(), func(a, b Node[int, string]) int { return a.ID - b.ID }) {
		if chainErr != nil {
			t.Fatal(chainErr)
		}
		chains = append(chains, chain)
	}
	if len(chains) != 1 || !slices.Equal(ids(chains[0]), []int{1, 2}) {
		t.Fatalf("canonical chains = %v, want [[1 2]]", chains)
	}

	_, err = collectWalk(WalkCanonical(context.Background(), source, 3))
	if !errors.Is(err, ErrNonCanonical) {
		t.Fatalf("WalkCanonical branch error = %v, want ErrNonCanonical", err)
	}
	branch, err := collectWalk(WalkFrom(context.Background(), source, 3))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids(branch), []int{3, 4}) {
		t.Fatalf("branch IDs = %v, want [3 4]", ids(branch))
	}
}

func TestChainsUsesComparatorOrder(t *testing.T) {
	source, err := NewMemorySource([]Node[int, string]{
		{ID: 20, ChildID: pointer(21)},
		{ID: 10},
		{ID: 21, ParentID: pointer(20)},
	})
	if err != nil {
		t.Fatal(err)
	}

	var roots []int
	for chain, chainErr := range source.Chains(context.Background(), func(a, b Node[int, string]) int { return b.ID - a.ID }) {
		if chainErr != nil {
			t.Fatal(chainErr)
		}
		roots = append(roots, chain[0].ID)
	}
	if !slices.Equal(roots, []int{20, 10}) {
		t.Fatalf("root order = %v, want [20 10]", roots)
	}
}

func TestNewMemorySourceRejectsDuplicateID(t *testing.T) {
	_, err := NewMemorySource([]Node[int, string]{{ID: 1}, {ID: 1}})
	if !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("error = %v, want ErrDuplicateID", err)
	}
}

func TestTraversalErrors(t *testing.T) {
	tests := []struct {
		name  string
		nodes []Node[int, string]
		start int
		walk  string
		want  error
	}{
		{"dangling child", []Node[int, string]{{ID: 1, ChildID: pointer(2)}}, 1, "forward", ErrDanglingChild},
		{"inconsistent backlink", []Node[int, string]{{ID: 1, ChildID: pointer(2)}, {ID: 2}}, 1, "forward", ErrInconsistentLink},
		{"forward cycle", []Node[int, string]{{ID: 1, ParentID: pointer(2), ChildID: pointer(2)}, {ID: 2, ParentID: pointer(1), ChildID: pointer(1)}}, 1, "forward", ErrCycle},
		{"dangling parent", []Node[int, string]{{ID: 2, ParentID: pointer(1)}}, 2, "canonical", ErrDanglingParent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := NewMemorySource(tt.nodes)
			if err != nil {
				t.Fatal(err)
			}
			if tt.walk == "canonical" {
				_, err = collectWalk(WalkCanonical(context.Background(), source, tt.start))
			} else {
				_, err = collectWalk(WalkFrom(context.Background(), source, tt.start))
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestChainsStopsAtFirstStructuralError(t *testing.T) {
	source, err := NewMemorySource([]Node[int, string]{
		{ID: 1, ChildID: pointer(2)},
		{ID: 10},
	})
	if err != nil {
		t.Fatal(err)
	}

	yields := 0
	for _, chainErr := range source.Chains(context.Background(), func(a, b Node[int, string]) int { return a.ID - b.ID }) {
		yields++
		if !errors.Is(chainErr, ErrDanglingChild) {
			t.Fatalf("error = %v, want ErrDanglingChild", chainErr)
		}
	}
	if yields != 1 {
		t.Fatalf("yield count = %d, want 1", yields)
	}
}

func TestEmptySourceAndContextCancellation(t *testing.T) {
	source, err := NewMemorySource([]Node[int, string]{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for range source.Chains(context.Background(), func(a, b Node[int, string]) int { return a.ID - b.ID }) {
		count++
	}
	if count != 0 {
		t.Fatalf("empty source yielded %d results", count)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = TailFrom(ctx, source, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestChainsRejectsNilComparator(t *testing.T) {
	source, err := NewMemorySource([]Node[int, string]{{ID: 1}})
	if err != nil {
		t.Fatal(err)
	}
	for _, chainErr := range source.Chains(context.Background(), nil) {
		if !errors.Is(chainErr, ErrNilComparator) {
			t.Fatalf("error = %v, want ErrNilComparator", chainErr)
		}
		return
	}
	t.Fatal("expected comparator error")
}
