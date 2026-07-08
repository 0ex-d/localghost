// Package apparedis is the box's apparition layer over Redis , data that appears just long enough
// to be useful and is gone on restart, which is exactly what an apparition is for. RESP2 over loopback, AUTH as an ACL user, one persistent
// mutex-serialised connection with reconnect-once on a broken pipe. It replaces the redis-cli
// shell-outs: no process spawn per command and no password on an argv.
//
// Not an ORM, not a framework: command in, reply out. Injection is impossible by CONSTRUCTION here,
// not by escaping: RESP frames every argument as a length-prefixed bulk string ($<len>\r\n<bytes>),
// so a value containing \r\n, quotes, or a whole other command is just bytes with a length , the
// server never parses values for structure. encodeRESP is separated out precisely so the test can
// prove that property with hostile inputs.
//
// Two client types, enforced at the TYPE level: ReadOnly exposes only read commands, ReadWrite embeds
// it and adds the writes. The Redis ACL (ghost_ro is +@read) is the real wall , the type system just
// makes the mistake uncompilable instead of a runtime error.
package apparedis

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Reply is one RESP2 reply. Exactly one of the fields is meaningful per type; Nil marks the RESP
// null bulk string ($-1), which Redis uses for "no such key".
type Reply struct {
	Str   string
	Int   int64
	Array []Reply
	Nil   bool
}

// Conn is the shared transport. Callers use ReadOnly/ReadWrite, not this directly.
type Conn struct {
	mu   sync.Mutex
	addr string
	user string // ACL user; empty = default user (1-arg AUTH)
	pass string
	c    net.Conn
	r    *bufio.Reader
}

func dialConn(addr, user, pass string) *Conn {
	return &Conn{addr: addr, user: user, pass: pass}
}

// connect (re)establishes the TCP connection and authenticates. Caller holds mu.
func (c *Conn) connect() error {
	if c.c != nil {
		_ = c.c.Close()
		c.c = nil
	}
	nc, err := net.DialTimeout("tcp", c.addr, 3*time.Second)
	if err != nil {
		return err
	}
	c.c = nc
	c.r = bufio.NewReader(nc)
	args := []string{"AUTH", c.pass}
	if c.user != "" {
		args = []string{"AUTH", c.user, c.pass}
	}
	if _, err := c.roundTrip(args); err != nil {
		_ = nc.Close()
		c.c = nil
		return fmt.Errorf("redis auth: %w", err)
	}
	return nil
}

// do sends one command and reads one reply, reconnecting once on a transport error. Retrying once is
// safe for this box's usage (idempotent gets/sets/list-trims); a second failure surfaces.
func (c *Conn) do(args []string) (Reply, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.c == nil {
		if err := c.connect(); err != nil {
			return Reply{}, err
		}
	}
	rep, err := c.roundTrip(args)
	if err != nil && isNetErr(err) {
		if cerr := c.connect(); cerr != nil {
			return Reply{}, cerr
		}
		return c.roundTrip(args)
	}
	return rep, err
}

func (c *Conn) roundTrip(args []string) (Reply, error) {
	_ = c.c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.c.Write(encodeRESP(args)); err != nil {
		return Reply{}, err
	}
	return readReply(c.r)
}

// encodeRESP frames a command as a RESP2 array of bulk strings. len() is BYTE length, and the value
// bytes are written verbatim inside the declared length , which is the whole injection story: the
// protocol has no in-band delimiters to escape, so there is nothing a hostile value can break out of.
func encodeRESP(args []string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(a), a)
	}
	return []byte(b.String())
}

func readReply(r *bufio.Reader) (Reply, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return Reply{}, err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if line == "" {
		return Reply{}, errors.New("empty reply line")
	}
	body := line[1:]
	switch line[0] {
	case '+':
		return Reply{Str: body}, nil
	case '-':
		return Reply{}, errors.New("redis: " + body)
	case ':':
		n, err := strconv.ParseInt(body, 10, 64)
		return Reply{Int: n}, err
	case '$':
		n, err := strconv.Atoi(body)
		if err != nil {
			return Reply{}, err
		}
		if n < 0 {
			return Reply{Nil: true}, nil
		}
		buf := make([]byte, n+2) // payload + CRLF
		if _, err := ioReadFull(r, buf); err != nil {
			return Reply{}, err
		}
		return Reply{Str: string(buf[:n])}, nil
	case '*':
		n, err := strconv.Atoi(body)
		if err != nil {
			return Reply{}, err
		}
		if n < 0 {
			return Reply{Nil: true}, nil
		}
		arr := make([]Reply, 0, n)
		for i := 0; i < n; i++ {
			el, err := readReply(r)
			if err != nil {
				return Reply{}, err
			}
			arr = append(arr, el)
		}
		return Reply{Array: arr}, nil
	default:
		return Reply{}, fmt.Errorf("unknown reply type %q", line[0])
	}
}

func ioReadFull(r *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func isNetErr(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) || errors.Is(err, net.ErrClosed) ||
		strings.Contains(err.Error(), "broken pipe") || strings.Contains(err.Error(), "connection reset")
}

// ReadOnly is the read client. It cannot express a write , there is no method for one, and the
// ghost_ro ACL user it authenticates as has +@read only, so even a future generic escape hatch would
// bounce off the server.
type ReadOnly struct {
	c *Conn
}

// NewReadOnly connects lazily as the read-only ACL user (127.0.0.1:port).
func NewReadOnly(port int, user, pass string) *ReadOnly {
	return &ReadOnly{c: dialConn("127.0.0.1:"+strconv.Itoa(port), user, pass)}
}

func (r *ReadOnly) Ping() error {
	_, err := r.c.do([]string{"PING"})
	return err
}

// Get returns the value and whether the key existed.
func (r *ReadOnly) Get(key string) (string, bool, error) {
	rep, err := r.c.do([]string{"GET", key})
	if err != nil {
		return "", false, err
	}
	return rep.Str, !rep.Nil, nil
}

func (r *ReadOnly) Exists(key string) (bool, error) {
	rep, err := r.c.do([]string{"EXISTS", key})
	return rep.Int > 0, err
}

func (r *ReadOnly) LRange(key string, start, stop int) ([]string, error) {
	rep, err := r.c.do([]string{"LRANGE", key, strconv.Itoa(start), strconv.Itoa(stop)})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rep.Array))
	for _, e := range rep.Array {
		out = append(out, e.Str)
	}
	return out, nil
}

func (r *ReadOnly) LLen(key string) (int64, error) {
	rep, err := r.c.do([]string{"LLEN", key})
	return rep.Int, err
}

// ReadWrite embeds ReadOnly and adds the writes. Authenticate it as the ghost_rw ACL user.
type ReadWrite struct {
	ReadOnly
}

func NewReadWrite(port int, user, pass string) *ReadWrite {
	return &ReadWrite{ReadOnly{c: dialConn("127.0.0.1:"+strconv.Itoa(port), user, pass)}}
}

func (w *ReadWrite) Set(key, val string) error {
	_, err := w.c.do([]string{"SET", key, val})
	return err
}

func (w *ReadWrite) Del(keys ...string) error {
	_, err := w.c.do(append([]string{"DEL"}, keys...))
	return err
}

func (w *ReadWrite) LPush(key string, vals ...string) error {
	_, err := w.c.do(append([]string{"LPUSH", key}, vals...))
	return err
}

func (w *ReadWrite) LTrim(key string, start, stop int) error {
	_, err := w.c.do([]string{"LTRIM", key, strconv.Itoa(start), strconv.Itoa(stop)})
	return err
}

func (w *ReadWrite) Expire(key string, seconds int) error {
	_, err := w.c.do([]string{"EXPIRE", key, strconv.Itoa(seconds)})
	return err
}

// Do is the generic escape hatch for commands without a helper , on the WRITE client only, and still
// subject to the server-side ACL. The read client deliberately has no equivalent.
func (w *ReadWrite) Do(args ...string) (Reply, error) { return w.c.do(args) }
