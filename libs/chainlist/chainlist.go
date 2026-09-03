// Package chainlist provides traversal primitives for collections of linked
// chains. Child links define the canonical forward path; parent links are used
// for reverse traversal and canonical-path validation.
package chainlist

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"
)

var (
	ErrDuplicateID      = errors.New("duplicate node ID")
	ErrNodeNotFound     = errors.New("node not found")
	ErrDanglingParent   = errors.New("dangling parent link")
	ErrDanglingChild    = errors.New("dangling child link")
	ErrCycle            = errors.New("chain cycle")
	ErrInconsistentLink = errors.New("inconsistent parent-child link")
	ErrNonCanonical     = errors.New("node is not on the canonical chain")
	ErrNilComparator    = errors.New("nil node comparator")
	ErrNilLoader        = errors.New("nil node loader")
)

// Node is one element in a chain. ChildID is authoritative when selecting the
// canonical forward path. A node whose ParentID points to a parent that selects
// another child is a non-canonical branch.
type Node[K comparable, V any] struct {
	ID       K
	ParentID *K
	ChildID  *K
	Value    V
}

// Loader resolves one node at a time. Implementations may load from memory or
// external persistence.
type Loader[K comparable, V any] interface {
	Load(context.Context, K) (Node[K, V], error)
}

// LoaderFunc adapts a function to Loader.
type LoaderFunc[K comparable, V any] func(context.Context, K) (Node[K, V], error)

func (f LoaderFunc[K, V]) Load(ctx context.Context, id K) (Node[K, V], error) {
	return f(ctx, id)
}

// ChainList is the common traversal interface implemented by an eagerly
// indexed MemorySource and a staged, loader-backed LazySource.
type ChainList[K comparable, V any] interface {
	WalkFrom(context.Context, K) iter.Seq2[Node[K, V], error]
	TailFrom(context.Context, K) (Node[K, V], error)
	WalkCanonical(context.Context, K) iter.Seq2[Node[K, V], error]
	Chains(context.Context, func(a, b Node[K, V]) int) iter.Seq2[[]Node[K, V], error]
}

// MemorySource indexes an unordered snapshot of nodes. It is immutable after
// construction and safe for concurrent traversal as long as stored values are
// themselves treated as immutable.
type MemorySource[K comparable, V any] struct {
	nodes map[K]Node[K, V]
}

// NewMemorySource creates an in-memory source. Duplicate IDs are rejected.
func NewMemorySource[K comparable, V any](nodes []Node[K, V]) (*MemorySource[K, V], error) {
	indexed := make(map[K]Node[K, V], len(nodes))
	for _, node := range nodes {
		if _, exists := indexed[node.ID]; exists {
			return nil, fmt.Errorf("%w: %v", ErrDuplicateID, node.ID)
		}
		indexed[node.ID] = cloneNode(node)
	}
	return &MemorySource[K, V]{nodes: indexed}, nil
}

// Load returns a copy of the requested node.
func (s *MemorySource[K, V]) Load(ctx context.Context, id K) (Node[K, V], error) {
	if err := ctx.Err(); err != nil {
		return Node[K, V]{}, err
	}
	node, exists := s.nodes[id]
	if !exists {
		return Node[K, V]{}, fmt.Errorf("%w: %v", ErrNodeNotFound, id)
	}
	return cloneNode(node), nil
}

// WalkFrom traverses the already indexed in-memory snapshot from startID.
func (s *MemorySource[K, V]) WalkFrom(ctx context.Context, startID K) iter.Seq2[Node[K, V], error] {
	return WalkFrom(ctx, s, startID)
}

// TailFrom returns the tail in the already indexed in-memory snapshot.
func (s *MemorySource[K, V]) TailFrom(ctx context.Context, startID K) (Node[K, V], error) {
	return TailFrom(ctx, s, startID)
}

// WalkCanonical traverses the complete canonical chain in the in-memory
// snapshot that contains nodeID.
func (s *MemorySource[K, V]) WalkCanonical(ctx context.Context, nodeID K) iter.Seq2[Node[K, V], error] {
	return WalkCanonical(ctx, s, nodeID)
}

// LazySource adapts a Loader to ChainList. It does not preload nodes: each
// traversal loads and yields one stage at a time. roots contains the canonical
// roots that should be exposed by Chains; single-chain operations do not need
// the target root to be registered here.
type LazySource[K comparable, V any] struct {
	loader Loader[K, V]
	roots  []K
}

// NewLazySource creates a staged chain list over loader. Root IDs may be given
// in any order because Chains orders their loaded nodes with its comparator.
func NewLazySource[K comparable, V any](loader Loader[K, V], roots []K) (*LazySource[K, V], error) {
	if loader == nil {
		return nil, ErrNilLoader
	}
	seen := make(map[K]struct{}, len(roots))
	rootCopy := make([]K, len(roots))
	for i, root := range roots {
		if _, exists := seen[root]; exists {
			return nil, fmt.Errorf("%w: root %v", ErrDuplicateID, root)
		}
		seen[root] = struct{}{}
		rootCopy[i] = root
	}
	return &LazySource[K, V]{loader: loader, roots: rootCopy}, nil
}

func (s *LazySource[K, V]) Load(ctx context.Context, id K) (Node[K, V], error) {
	return s.loader.Load(ctx, id)
}

// WalkFrom loads and yields one node at a time from startID to its tail.
func (s *LazySource[K, V]) WalkFrom(ctx context.Context, startID K) iter.Seq2[Node[K, V], error] {
	return WalkFrom(ctx, s, startID)
}

// TailFrom incrementally follows child links without loading unrelated nodes.
func (s *LazySource[K, V]) TailFrom(ctx context.Context, startID K) (Node[K, V], error) {
	return TailFrom(ctx, s, startID)
}

// WalkCanonical incrementally resolves the root and then yields its canonical
// chain one node at a time.
func (s *LazySource[K, V]) WalkCanonical(ctx context.Context, nodeID K) iter.Seq2[Node[K, V], error] {
	return WalkCanonical(ctx, s, nodeID)
}

// Chains resolves the configured roots, orders them with compare, and then
// loads each chain incrementally. An individual []Node is materialized only for
// the chain currently being yielded.
func (s *LazySource[K, V]) Chains(ctx context.Context, compare func(a, b Node[K, V]) int) iter.Seq2[[]Node[K, V], error] {
	return func(yield func([]Node[K, V], error) bool) {
		if compare == nil {
			yield(nil, ErrNilComparator)
			return
		}

		roots := make([]Node[K, V], 0, len(s.roots))
		for _, rootID := range s.roots {
			if err := ctx.Err(); err != nil {
				yield(nil, err)
				return
			}
			root, err := s.Load(ctx, rootID)
			if err != nil {
				yield(nil, err)
				return
			}
			if root.ID != rootID || root.ParentID != nil {
				yield(nil, fmt.Errorf("%w: configured root %v is not a root", ErrInconsistentLink, rootID))
				return
			}
			roots = append(roots, root)
		}
		slices.SortFunc(roots, compare)

		for _, root := range roots {
			chain := make([]Node[K, V], 0, 1)
			for node, err := range s.WalkFrom(ctx, root.ID) {
				if err != nil {
					yield(nil, err)
					return
				}
				chain = append(chain, node)
			}
			if !yield(chain, nil) {
				return
			}
		}
	}
}

// WalkFrom lazily walks from startID to the tail by following authoritative
// child links. It trusts that startID is on the desired branch, but validates
// every traversed child-to-parent backlink.
func WalkFrom[K comparable, V any](ctx context.Context, loader Loader[K, V], startID K) iter.Seq2[Node[K, V], error] {
	return func(yield func(Node[K, V], error) bool) {
		currentID := startID
		seen := make(map[K]struct{})
		var expectedParent *K
		first := true

		for {
			if err := ctx.Err(); err != nil {
				yield(Node[K, V]{}, err)
				return
			}
			if _, exists := seen[currentID]; exists {
				yield(Node[K, V]{}, fmt.Errorf("%w at node %v", ErrCycle, currentID))
				return
			}

			node, err := loader.Load(ctx, currentID)
			if err != nil {
				if first {
					yield(Node[K, V]{}, err)
				} else {
					yield(Node[K, V]{}, fmt.Errorf("%w: child %v: %w", ErrDanglingChild, currentID, err))
				}
				return
			}
			if node.ID != currentID {
				yield(Node[K, V]{}, fmt.Errorf("%w: requested %v, loader returned %v", ErrInconsistentLink, currentID, node.ID))
				return
			}
			if expectedParent != nil && (node.ParentID == nil || *node.ParentID != *expectedParent) {
				yield(Node[K, V]{}, fmt.Errorf("%w: child %v does not point back to parent %v", ErrInconsistentLink, node.ID, *expectedParent))
				return
			}

			seen[node.ID] = struct{}{}
			if !yield(cloneNode(node), nil) {
				return
			}
			if node.ChildID == nil {
				return
			}
			parent := node.ID
			expectedParent = &parent
			currentID = *node.ChildID
			first = false
		}
	}
}

// TailFrom returns the tail reached by WalkFrom.
func TailFrom[K comparable, V any](ctx context.Context, loader Loader[K, V], startID K) (Node[K, V], error) {
	var tail Node[K, V]
	for node, err := range WalkFrom(ctx, loader, startID) {
		if err != nil {
			return Node[K, V]{}, err
		}
		tail = node
	}
	return tail, nil
}

// WalkCanonical first walks through parent links to find the root and verifies
// that each parent selected the requested ancestry through ChildID. It then
// yields the complete canonical chain from root to tail.
func WalkCanonical[K comparable, V any](ctx context.Context, loader Loader[K, V], nodeID K) iter.Seq2[Node[K, V], error] {
	return func(yield func(Node[K, V], error) bool) {
		rootID, err := canonicalRoot(ctx, loader, nodeID)
		if err != nil {
			yield(Node[K, V]{}, err)
			return
		}

		found := false
		for node, walkErr := range WalkFrom(ctx, loader, rootID) {
			if walkErr != nil {
				yield(Node[K, V]{}, walkErr)
				return
			}
			if node.ID == nodeID {
				found = true
			}
			if !yield(node, nil) {
				return
			}
		}
		if !found {
			yield(Node[K, V]{}, fmt.Errorf("%w: %v", ErrNonCanonical, nodeID))
		}
	}
}

func canonicalRoot[K comparable, V any](ctx context.Context, loader Loader[K, V], nodeID K) (K, error) {
	currentID := nodeID
	seen := make(map[K]struct{})
	for {
		if err := ctx.Err(); err != nil {
			return *new(K), err
		}
		if _, exists := seen[currentID]; exists {
			return *new(K), fmt.Errorf("%w at node %v", ErrCycle, currentID)
		}
		seen[currentID] = struct{}{}

		node, err := loader.Load(ctx, currentID)
		if err != nil {
			if currentID == nodeID {
				return *new(K), err
			}
			return *new(K), fmt.Errorf("%w: parent %v: %w", ErrDanglingParent, currentID, err)
		}
		if node.ID != currentID {
			return *new(K), fmt.Errorf("%w: requested %v, loader returned %v", ErrInconsistentLink, currentID, node.ID)
		}
		if node.ParentID == nil {
			return node.ID, nil
		}

		parentID := *node.ParentID
		parent, err := loader.Load(ctx, parentID)
		if err != nil {
			return *new(K), fmt.Errorf("%w: parent %v of node %v: %w", ErrDanglingParent, parentID, node.ID, err)
		}
		if parent.ID != parentID {
			return *new(K), fmt.Errorf("%w: requested %v, loader returned %v", ErrInconsistentLink, parentID, parent.ID)
		}
		if parent.ChildID == nil || *parent.ChildID != node.ID {
			return *new(K), fmt.Errorf("%w: parent %v does not select child %v", ErrNonCanonical, parent.ID, node.ID)
		}
		currentID = parentID
	}
}

// Chains yields every canonical root-to-tail chain in comparator order. Nodes
// on non-canonical branches are intentionally omitted. The first structural
// error is yielded and stops iteration.
func (s *MemorySource[K, V]) Chains(ctx context.Context, compare func(a, b Node[K, V]) int) iter.Seq2[[]Node[K, V], error] {
	return func(yield func([]Node[K, V], error) bool) {
		if compare == nil {
			yield(nil, ErrNilComparator)
			return
		}
		if err := ctx.Err(); err != nil {
			yield(nil, err)
			return
		}

		roots := make([]Node[K, V], 0)
		for _, node := range s.nodes {
			if node.ParentID == nil {
				roots = append(roots, cloneNode(node))
			}
		}
		slices.SortFunc(roots, compare)

		accounted := make(map[K]struct{}, len(s.nodes))
		for _, root := range roots {
			chain := make([]Node[K, V], 0, 1)
			for node, err := range WalkFrom(ctx, s, root.ID) {
				if err != nil {
					yield(nil, err)
					return
				}
				chain = append(chain, node)
				accounted[node.ID] = struct{}{}
			}
			if !yield(chain, nil) {
				return
			}
		}

		// A node rejected by an existing parent is a permissible losing branch.
		// Ignore it and every node whose parent ancestry descends from it.
		ignored := make(map[K]struct{})
		changed := true
		for changed {
			changed = false
			for id, node := range s.nodes {
				if _, done := accounted[id]; done {
					continue
				}
				if _, done := ignored[id]; done {
					continue
				}
				if node.ParentID == nil {
					continue
				}
				if _, parentIgnored := ignored[*node.ParentID]; parentIgnored {
					ignored[id] = struct{}{}
					changed = true
					continue
				}
				parent, exists := s.nodes[*node.ParentID]
				if exists && (parent.ChildID == nil || *parent.ChildID != id) {
					ignored[id] = struct{}{}
					changed = true
				}
			}
		}

		remaining := make([]Node[K, V], 0)
		for id, node := range s.nodes {
			if _, done := accounted[id]; done {
				continue
			}
			if _, branch := ignored[id]; branch {
				continue
			}
			remaining = append(remaining, cloneNode(node))
		}
		if len(remaining) == 0 {
			return
		}
		slices.SortFunc(remaining, compare)
		candidate := remaining[0]
		if candidate.ParentID != nil {
			if _, exists := s.nodes[*candidate.ParentID]; !exists {
				yield(nil, fmt.Errorf("%w: parent %v of node %v", ErrDanglingParent, *candidate.ParentID, candidate.ID))
				return
			}
		}
		// With roots and non-canonical branches removed, any remaining closed
		// component has no root and therefore contains a cycle.
		yield(nil, fmt.Errorf("%w at node %v", ErrCycle, candidate.ID))
	}
}

func cloneNode[K comparable, V any](node Node[K, V]) Node[K, V] {
	cloned := node
	if node.ParentID != nil {
		parent := *node.ParentID
		cloned.ParentID = &parent
	}
	if node.ChildID != nil {
		child := *node.ChildID
		cloned.ChildID = &child
	}
	return cloned
}
