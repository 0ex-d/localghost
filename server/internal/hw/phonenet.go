package hw

import (
	"strconv"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/egress"
)

// PhoneNetFrom reads what the phone last said about its network (settings phone_net, phone_seen,
// written by secd's /v1/phone/net): the daemons' fetch loops ask it whether the phone is fetching
// for them.
func PhoneNetFrom(c Querier) egress.PhoneNet {
	var p egress.PhoneNet
	rows, err := c.Query("SELECT key, value FROM settings WHERE key IN ('phone_net', 'phone_seen')")
	if err != nil {
		return p
	}
	for _, v := range rows.Vals {
		if len(v) < 2 || v[0] == nil || v[1] == nil {
			continue
		}
		switch *v[0] {
		case "phone_net":
			p.Net = *v[1]
		case "phone_seen":
			if n, err := strconv.ParseInt(*v[1], 10, 64); err == nil && n > 0 {
				p.Seen = time.Unix(n, 0)
			}
		}
	}
	return p
}
