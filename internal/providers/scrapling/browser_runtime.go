package scrapling

import _ "embed"

// browserRuntimeScript is prepended to the subprocess adapter when browser mode
// is requested. Keeping it separate allows offline protocol regression tests.
//
//go:embed browser_runtime.py
var browserRuntimeScript string
