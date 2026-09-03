package graph

import (
	"dtm/chain"
	"dtm/db/db"
	"dtm/mq/mq"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.
// ex: put your database connection or HTTP client in here.

type Resolver struct {
	TripDB                  db.TripDBWrapper
	ChainStore              chain.Store
	TripMessageQueueWrapper mq.TripMessageQueueWrapper
}

func (r *Resolver) recordChainStore() chain.Store {
	if r.ChainStore != nil {
		return r.ChainStore
	}
	return r.TripDB
}
