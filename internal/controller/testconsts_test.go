package controller

// Shared literals reused across multiple *_test.go files in this package
// (goconst flags duplicate string literals package-wide, not just per-file).
const (
	testDoesNotExistInstanceName = "does-not-exist"
	testSecretAPIKeyField        = "apikey"
	testSecretConfigField        = "key"
)
