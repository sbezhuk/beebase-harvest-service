package harvest

import "errors"

// ErrNotFound is returned when no harvest matches the given ID under the
// given hive. Note this is distinct from hive ownership itself, which
// application/harvest.HiveVerifier checks against hive-service before a
// repository call is ever made - by the time a Repository method runs,
// the caller has already been confirmed to own hiveID.
var ErrNotFound = errors.New("harvest not found")
