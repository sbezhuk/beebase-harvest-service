package harvest

import "errors"

// ErrHiveNotFound is returned when a hiveID doesn't exist, doesn't belong
// to the caller (which also covers its apiary not belonging to the
// caller, since hive-service's own ownership check is transitive), or its
// ownership couldn't be confirmed. As with harvest.ErrNotFound, these
// cases are deliberately indistinguishable: a caller must not be able to
// tell whether another user's hive ID exists at all.
var ErrHiveNotFound = errors.New("hive not found")

// ErrHiveReadOnly is returned when a free-tier user attempts to create or
// update a harvest whose hive currently falls outside their Free
// entitlement (see hive-service's writable selection) - i.e. the hive
// itself, or its own parent apiary, requires Pro. Distinct from
// ErrHiveNotFound: the hive exists and belongs to the caller, it's simply
// not writable right now.
var ErrHiveReadOnly = errors.New("hive is read-only under the free plan")
