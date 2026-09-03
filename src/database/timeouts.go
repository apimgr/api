package database

import "time"

// Per-class query timeouts mandated by AI.md PART 10: every query and
// transaction carries a deadline so a stalled connection can never wedge a
// caller indefinitely.
const (
	// timeoutSimpleSelect bounds a single-row or narrow indexed read.
	timeoutSimpleSelect = 5 * time.Second

	// timeoutComplexSelect bounds a multi-table or scanning read.
	timeoutComplexSelect = 15 * time.Second

	// timeoutWrite bounds a single INSERT, UPDATE, or DELETE.
	timeoutWrite = 10 * time.Second

	// timeoutBulk bounds a sweep that may touch a large number of rows.
	timeoutBulk = 60 * time.Second

	// timeoutSchema bounds idempotent DDL applied on every startup.
	timeoutSchema = 5 * time.Minute
)
