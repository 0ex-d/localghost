package secd

import (
	"strconv"
	"time"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

var modTimeZero = time.Time{}
