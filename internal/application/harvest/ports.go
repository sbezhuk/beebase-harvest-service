package harvest

import (
	"context"

	"github.com/google/uuid"
)

// HiveVerifier confirms that a hive belongs to whoever presented
// accessToken. It's a port because hives (and, transitively, apiaries)
// live in a different service with its own database; this service never
// queries hive or apiary ownership itself, it only ever asks hive-service.
//
// Unlike inspection-service's own HiveVerifier - which is only consulted
// once, at creation time, because inspection-service then denormalizes
// the verified owner onto the inspection row - this service never stores
// hive ownership data of its own (ticket requirement: no duplicated
// ownership, no cross-database foreign key to hive-service's database).
// So every use case in application/harvest calls Verify on every
// operation (create, get, list, update, delete), not just on create.
type HiveVerifier interface {
	Verify(ctx context.Context, accessToken string, hiveID uuid.UUID) error
}

// OwnedHiveLister is implemented by the hive-service client used for the
// cross-hive harvest list. It deliberately remains separate from
// HiveVerifier so hive-scoped use cases only need the narrow verification
// capability they already had.
type OwnedHiveLister interface {
	ListOwned(ctx context.Context, accessToken string) ([]uuid.UUID, error)
}
