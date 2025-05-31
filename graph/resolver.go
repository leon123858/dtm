package graph

import (
	"dtm/db/db"
	"dtm/graph/loader"
	"dtm/mq/mq"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.
// ex: put your database connection or HTTP client in here.

type Resolver struct {
	TripDB                  db.TripDBWrapper
	TripMessageQueueWrapper mq.TripMessageQueueWrapper
	TripDataLoader          loader.TripDataLoader
}
