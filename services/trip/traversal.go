package trip

import (
	"context"
	"fmt"

	"dtm/adapters/db/db"
	"dtm/domain"
	"dtm/libs/chainlist"

	"github.com/google/uuid"
)

// scanChains validates the complete history already loaded by the reader.
func scanChains(ctx context.Context, records []db.RecordSnapshot) ([][]chainlist.Node[uuid.UUID, domain.RecordInfo], error) {
	infos := make([]domain.RecordInfo, len(records))
	for i, record := range records {
		infos[i] = record.RecordInfo
	}

	nodes := make([]chainlist.Node[uuid.UUID, domain.RecordInfo], len(infos))
	order := make(map[uuid.UUID]int, len(infos))
	for i, info := range infos {
		nodes[i] = infoNode(info)
		if _, ok := order[info.ID]; !ok {
			order[info.ID] = i
		}
	}
	source, err := chainlist.NewMemorySource(nodes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidChain, err)
	}
	var chains [][]chainlist.Node[uuid.UUID, domain.RecordInfo]
	for c, walkErr := range source.Chains(ctx, func(a, b chainlist.Node[uuid.UUID, domain.RecordInfo]) int { return order[a.ID] - order[b.ID] }) {
		if walkErr != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidChain, walkErr)
		}
		chains = append(chains, c)
	}
	return chains, nil
}

func activeTailIDs(chains [][]chainlist.Node[uuid.UUID, domain.RecordInfo]) map[uuid.UUID]bool {
	active := make(map[uuid.UUID]bool, len(chains))
	for _, c := range chains {
		if len(c) > 0 {
			active[c[len(c)-1].ID] = true
		}
	}
	return active
}

func infoNode(info domain.RecordInfo) chainlist.Node[uuid.UUID, domain.RecordInfo] {
	return chainlist.Node[uuid.UUID, domain.RecordInfo]{ID: info.ID, ParentID: info.ParentRecordID, ChildID: info.ChildRecordID, Value: info}
}
