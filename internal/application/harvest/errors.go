package harvest

import "errors"

// ErrHiveNotFound is returned when a hiveID doesn't exist, doesn't belong
// to the caller (which also covers its apiary not belonging to the
// caller, since hive-service's own ownership check is transitive), or its
// ownership couldn't be confirmed. As with harvest.ErrNotFound, these
// cases are deliberately indistinguishable: a caller must not be able to
// tell whether another user's hive ID exists at all.
var ErrHiveNotFound = errors.New("hive not found")
