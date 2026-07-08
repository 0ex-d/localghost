package framed

import "strconv"

// Small text-cell converters for poltergres's text-format results. A malformed cell yields the zero value
// , the query shapes are ours and typed, so this is belt-not-braces.

func atoi64(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }
func atof(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
