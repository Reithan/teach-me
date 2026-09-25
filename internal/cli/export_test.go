package cli

// BuildEventForID exports the unexported buildEventForID for use by external
// test packages (migrate_test.go). Only compiled into the test binary.
var BuildEventForID = buildEventForID
