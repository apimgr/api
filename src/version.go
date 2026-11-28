package main

import (
	"strconv"
	"time"
)

var (
	Version      = "devel"
	CommitID     = "N/A"
	BuildDate    = "N/A"
	BuildEpoch   = "0"
	OfficialSite = ""
)

const exConfig = 78
const exUsage = 2

func init() {
	if epoch := buildEpoch(); epoch > 0 {
		BuildDate = time.Unix(epoch, 0).UTC().Format("2006-01-02T15:04:05Z")
	}
}

// buildEpoch parses the embedded Unix timestamp, returning zero when unset.
func buildEpoch() int64 {
	epoch, err := strconv.ParseInt(BuildEpoch, 10, 64)
	if err != nil {
		return 0
	}
	return epoch
}
