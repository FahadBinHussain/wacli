package mediamembership

// Exceeding a limit refuses the entire membership; it never drops references.
const (
	MaxReferences              = 1024
	MaxStagedDescriptors       = 1024
	MaxTotalStagedBytes  int64 = 256 << 20
)
