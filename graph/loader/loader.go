package loader

import (
	"dtm/db/db"

	"github.com/google/uuid"
	"github.com/vikstrous/dataloadgen"
)

type TripDataLoader struct {
	RecordLoader *dataloadgen.Loader[uuid.UUID, db.Record]
}
