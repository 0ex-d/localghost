package ctlsock

// Client dials a daemon's control socket. Used by ghost-cli (operator) and by daemons talking to each
// other (watchd telling a service to change log level; watchd asking ghost.oracled to evaluate logs).
// One connection per call , the sockets are local and calls are infrequent, so no pooling.

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"time"
)

type Client struct {
	sockPath string
	timeout  time.Duration
}

// NewClient targets <runDir>/<service>.sock. timeout bounds a call; a caller with a hard deadline
// (watchd's log triage) passes a short one via NewClientTimeout.
func NewClient(service, runDir string) *Client {
	return &Client{sockPath: filepath.Join(runDir, service+".sock"), timeout: 30 * time.Second}
}

// NewClientTimeout is NewClient with an explicit call timeout.
func NewClientTimeout(service, runDir string, timeout time.Duration) *Client {
	c := NewClient(service, runDir)
	c.timeout = timeout
	return c
}

// Call sends one command with optional args and returns the response. args may be nil.
func (c *Client) Call(cmd string, args any) (Response, error) {
	var raw json.RawMessage
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			return Response{}, err
		}
		raw = b
	}
	d := net.Dialer{Timeout: 3 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	conn, err := d.DialContext(ctx, "unix", c.sockPath)
	if err != nil {
		return Response{}, fmt.Errorf("dial %s: %w", c.sockPath, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(c.timeout))
	if err := json.NewEncoder(conn).Encode(Request{Cmd: cmd, Args: raw}); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, err
	}
	if !resp.OK {
		return resp, fmt.Errorf("%s", resp.Err)
	}
	return resp, nil
}
