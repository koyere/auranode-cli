// Package tunnel implements both ends of a CLI tunnel over a WebSocket to the
// backend, with the same half-close, draining and reset-on-saturation handling as the
// agent:
//   - source role (Type 1, local): opens a TCP listener on the user machine and
//     multiplexes each connection to the destination agent, which dials.
//   - dest role (Type 2 reverse): the source agent opens the public listener on the VPS
//     and the backend relays each incoming connection to the CLI, which dials a local
//     service of the user (ngrok/webhooks case). It mirrors the agent dest role.
package tunnel

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Tunnel protocol message types (kept in sync with the backend).
const (
	typeTunnelOpen    = "tunnel_open"
	typeTunnelOpenAck = "tunnel_open_ack"
	typeTunnelData    = "tunnel_data"
	typeTunnelClose   = "tunnel_close"
	typeTunnelWindow  = "tunnel_window"
	typeTunnelStop    = "tunnel_stop"
	typeTunnelReady   = "tunnel_ready"
)

const (
	ackTimeout  = 15 * time.Second
	relayBuf    = 32 * 1024
	inboxBuffer = 2048
	writeWait   = 10 * time.Second
	pongWait    = 70 * time.Second
	// windowSize: initial credits (in-flight bytes) per stream and direction. Same
	// flow control as the agent: the sender does not read beyond the window without
	// credit; the receiver grants credit (tunnel_window) as it drains.
	windowSize = 256 * 1024
)

type msg struct {
	Type       string `json:"type"`
	TunnelID   string `json:"tunnel_id,omitempty"`
	StreamID   string `json:"stream_id,omitempty"`
	Data       string `json:"data,omitempty"`
	Error      string `json:"error,omitempty"`
	OK         bool   `json:"ok,omitempty"`
	FC         bool   `json:"fc,omitempty"`         // flow-control capability (open/open_ack)
	Host       string `json:"host,omitempty"`       // backend→dest: where to dial
	Port       int    `json:"port,omitempty"`       // backend→dest
	LocalPort  int    `json:"local_port,omitempty"` // tunnel_ready
	RemoteHost string `json:"remote_host,omitempty"`
	RemotePort int    `json:"remote_port,omitempty"`
	Bytes      int    `json:"bytes,omitempty"` // tunnel_window: granted credit
	Timestamp  int64  `json:"timestamp,omitempty"`
}

// role distinguishes the CLI role within the tunnel.
type role int

const (
	roleSource role = iota // Type 1: the CLI listens locally
	roleDest               // Type 2 reverse: the CLI dials the local service
)

// Client holds the session of a CLI tunnel.
type Client struct {
	tunnelID string
	role     role
	// localAddr (source): address to listen on locally.
	// dialAddr  (dest):   address of the local service to dial.
	localAddr string
	dialAddr  string
	logf      func(format string, a ...any)

	conn    *websocket.Conn
	writeMu sync.Mutex

	mu      sync.Mutex
	streams map[string]*stream

	readyCh chan msg
}

type stream struct {
	streamID string
	conn     net.Conn
	inbox    chan []byte
	done     chan struct{}
	ready    chan struct{}
	failed   chan struct{}

	closeOnce   sync.Once
	inboxOnce   sync.Once
	inboxClosed atomic.Bool
	stateMu     sync.Mutex
	readDone  bool
	writeDone bool

	fc         bool // both ends support credits → gating active
	creditMu   sync.Mutex
	creditCond *sync.Cond
	sendCredit int
}

func (s *stream) abort() {
	s.closeOnce.Do(func() {
		close(s.done)
		if s.conn != nil {
			s.conn.Close()
		}
		if s.creditCond != nil {
			s.creditCond.Broadcast()
		}
	})
}

func (s *stream) closeInbox() {
	s.inboxOnce.Do(func() { s.inboxClosed.Store(true); close(s.inbox) })
}

// initFlow initializes flow control with the full window.
func (s *stream) initFlow() {
	s.creditCond = sync.NewCond(&s.creditMu)
	s.sendCredit = windowSize
}

// takeCredit blocks until there is credit (or the stream ends) and reserves up to
// `max` bytes. Returns 0 if the stream is closed.
func (s *stream) takeCredit(max int) int {
	s.creditMu.Lock()
	defer s.creditMu.Unlock()
	for s.sendCredit <= 0 {
		select {
		case <-s.done:
			return 0
		default:
		}
		s.creditCond.Wait()
	}
	select {
	case <-s.done:
		return 0
	default:
	}
	n := s.sendCredit
	if n > max {
		n = max
	}
	s.sendCredit -= n
	return n
}

func (s *stream) addCredit(n int) {
	s.creditMu.Lock()
	s.sendCredit += n
	s.creditMu.Unlock()
	s.creditCond.Broadcast()
}

// New creates a source-role client (Type 1): listens on localAddr and forwards to the agent.
// apiURL is the API HTTP URL (with or without /api/v1).
func New(apiURL, token, tunnelID, localAddr string, logf func(string, ...any)) (*Client, error) {
	conn, err := dialTunnel(apiURL, token, tunnelID)
	if err != nil {
		return nil, err
	}
	return &Client{
		tunnelID:  tunnelID,
		role:      roleSource,
		localAddr: localAddr,
		logf:      logf,
		conn:      conn,
		streams:   make(map[string]*stream),
		readyCh:   make(chan msg, 1),
	}, nil
}

// NewDest creates a dest-role client (Type 2 reverse): for each connection the backend
// relays, it dials the local service at dialAddr.
func NewDest(apiURL, token, tunnelID, dialAddr string, logf func(string, ...any)) (*Client, error) {
	conn, err := dialTunnel(apiURL, token, tunnelID)
	if err != nil {
		return nil, err
	}
	return &Client{
		tunnelID: tunnelID,
		role:     roleDest,
		dialAddr: dialAddr,
		logf:     logf,
		conn:     conn,
		streams:  make(map[string]*stream),
		readyCh:  make(chan msg, 1),
	}, nil
}

// dialTunnel opens the authenticated WebSocket to the backend tunnel endpoint.
func dialTunnel(apiURL, token, tunnelID string) (*websocket.Conn, error) {
	wsURL, err := wsTunnelURL(apiURL, token, tunnelID)
	if err != nil {
		return nil, err
	}
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, resp, err := dialer.Dial(wsURL, http.Header{})
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("could not open the session (HTTP %d): %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("could not connect to the backend: %w", err)
	}
	return conn, nil
}

// Run opens the local listener and relays until ctx is canceled, the backend closes the
// session (tunnel_stop) or the WebSocket drops. Calls onReady once the session is ready.
func (c *Client) Run(ctx context.Context, onReady func(localPort, remotePort int, remoteHost string)) error {
	defer c.conn.Close()

	c.conn.SetReadLimit(9 << 20)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	go c.pinger(ctx)

	// Wait for tunnel_ready before opening the listener.
	readErr := make(chan error, 1)
	go c.readLoop(readErr)

	select {
	case <-ctx.Done():
		return nil
	case err := <-readErr:
		return err
	case ready := <-c.readyCh:
		if onReady != nil {
			onReady(ready.LocalPort, ready.RemotePort, ready.RemoteHost)
		}
	case <-time.After(ackTimeout):
		return fmt.Errorf("the backend did not confirm the session in time")
	}

	// Source role: open the local listener and accept connections. Dest role: there is no
	// local listener — the dial is triggered by each tunnel_open that arrives via readLoop.
	if c.role == roleSource {
		ln, err := net.Listen("tcp", c.localAddr)
		if err != nil {
			return fmt.Errorf("could not open local port %s: %w", c.localAddr, err)
		}
		defer ln.Close()
		go c.acceptLoop(ln)
	}

	select {
	case <-ctx.Done():
	case err := <-readErr:
		c.shutdownStreams()
		return err
	}
	c.shutdownStreams()
	return nil
}

func (c *Client) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		s := &stream{
			streamID: randomID(),
			conn:     conn,
			inbox:    make(chan []byte, inboxBuffer),
			done:     make(chan struct{}),
			ready:    make(chan struct{}),
			failed:   make(chan struct{}),
		}
		s.initFlow()
		c.register(s)
		c.send(msg{Type: typeTunnelOpen, TunnelID: c.tunnelID, StreamID: s.streamID, FC: true})
		go c.sourceStream(s)
	}
}

func (c *Client) sourceStream(s *stream) {
	select {
	case <-s.ready:
		c.relay(s)
	case <-s.failed:
		s.abort()
		c.unregister(s.streamID)
	case <-time.After(ackTimeout):
		c.send(msg{Type: typeTunnelClose, TunnelID: c.tunnelID, StreamID: s.streamID, Error: "ack timeout"})
		s.abort()
		c.unregister(s.streamID)
	case <-s.done:
		c.unregister(s.streamID)
	}
}

// openDest (dest role) dials the local service and, on success, starts the relay.
// It prefers dialAddr (the user --to); if empty it uses the host:port from tunnel_open
// (the remote_host/remote_port stored in the tunnel). peerFC indicates whether the
// source supports flow control (gating is enabled only if both support it).
func (c *Client) openDest(streamID, host string, port int, peerFC bool) {
	addr := c.dialAddr
	if addr == "" {
		addr = fmt.Sprintf("%s:%d", host, port)
	}
	s := &stream{
		streamID: streamID,
		inbox:    make(chan []byte, inboxBuffer),
		done:     make(chan struct{}),
		ready:    make(chan struct{}),
		failed:   make(chan struct{}),
		fc:       peerFC,
	}
	s.initFlow()
	c.register(s)

	go func() {
		conn, err := net.DialTimeout("tcp", addr, ackTimeout)
		if err != nil {
			c.logf("dial to %s failed: %v", addr, err)
			c.send(msg{Type: typeTunnelOpenAck, TunnelID: c.tunnelID, StreamID: streamID, OK: false, Error: err.Error()})
			c.unregister(streamID)
			return
		}
		s.conn = conn
		// FC: peerFC echoes the negotiated capability (clean fallback if the flag did not
		// arrive from an older backend).
		c.send(msg{Type: typeTunnelOpenAck, TunnelID: c.tunnelID, StreamID: streamID, OK: true, FC: peerFC})
		c.relay(s)
	}()
}

func (c *Client) readLoop(readErr chan<- error) {
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			readErr <- nil // normal WS close: end of session
			return
		}
		c.conn.SetReadDeadline(time.Now().Add(pongWait))

		var m msg
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.Type {
		case typeTunnelReady:
			select {
			case c.readyCh <- m:
			default:
			}
		case typeTunnelOpen:
			// Only the dest role receives tunnel_open: it dials the local service.
			if c.role == roleDest {
				c.openDest(m.StreamID, m.Host, m.Port, m.FC)
			}
		case typeTunnelOpenAck:
			c.ack(m.StreamID, m.OK, m.FC)
		case typeTunnelData:
			b, e := base64.StdEncoding.DecodeString(m.Data)
			if e == nil {
				c.data(m.StreamID, b)
			}
		case typeTunnelClose:
			c.closeStream(m.StreamID)
		case typeTunnelWindow:
			c.addCredit(m.StreamID, m.Bytes)
		case typeTunnelStop:
			readErr <- nil // the backend closed the tunnel
			return
		}
	}
}

func (c *Client) ack(streamID string, ok, peerFC bool) {
	c.mu.Lock()
	s := c.streams[streamID]
	c.mu.Unlock()
	if s == nil {
		return
	}
	if ok {
		s.fc = peerFC // set before closing ready (the relay starts afterwards)
		safeClose(s.ready)
	} else {
		safeClose(s.failed)
	}
}

func (c *Client) data(streamID string, b []byte) {
	c.mu.Lock()
	s := c.streams[streamID]
	c.mu.Unlock()
	if s == nil || s.inboxClosed.Load() {
		return // stream closed in this direction: do not send (avoids panic)
	}
	select {
	case <-s.done:
	case s.inbox <- b:
	default:
		// Buffer saturated: reset the stream (without dropping bytes mid-stream).
		c.send(msg{Type: typeTunnelClose, TunnelID: c.tunnelID, StreamID: streamID, Error: "inbox overflow"})
		s.abort()
		c.unregister(streamID)
	}
}

func (c *Client) addCredit(streamID string, bytes int) {
	c.mu.Lock()
	s := c.streams[streamID]
	c.mu.Unlock()
	if s == nil || bytes <= 0 {
		return
	}
	s.addCredit(bytes)
}

// closeStream closes the peer→local direction (half-close). It does NOT remove the stream
// from the map: the local→peer direction may still be active and needs to receive credits.
// The stream is removed via markDone once both directions finish.
func (c *Client) closeStream(streamID string) {
	c.mu.Lock()
	s := c.streams[streamID]
	c.mu.Unlock()
	if s != nil {
		s.closeInbox()
	}
}

func (c *Client) relay(s *stream) {
	// Writer (peer→local): inbox → conn; on orderly EOF, drain and half-close.
	go func() {
		for {
			select {
			case <-s.done:
				return
			case b, ok := <-s.inbox:
				if !ok {
					if tcp, isTCP := s.conn.(*net.TCPConn); isTCP {
						tcp.CloseWrite()
					} else {
						s.conn.Close()
					}
					c.markDone(s, false)
					return
				}
				if _, err := s.conn.Write(b); err != nil {
					s.abort()
					c.unregister(s.streamID)
					return
				}
				// Grant credit to the opposite sender: we already drained len(b).
				if s.fc {
					c.send(msg{Type: typeTunnelWindow, TunnelID: c.tunnelID, StreamID: s.streamID, Bytes: len(b)})
				}
			}
		}
	}()

	// Reader (local→peer): conn → tunnel_data. With flow control (s.fc) it waits for credit
	// before reading → backpressure to the origin if the opposite receiver is slow. Without fc (older
	// peer) it reads freely. On EOF it signals the end of the direction.
	buf := make([]byte, relayBuf)
	for {
		budget := len(buf)
		if s.fc {
			budget = s.takeCredit(len(buf))
			if budget == 0 {
				return // stream closed while waiting for credit
			}
		}
		n, err := s.conn.Read(buf[:budget])
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			c.send(msg{Type: typeTunnelData, TunnelID: c.tunnelID, StreamID: s.streamID,
				Data: base64.StdEncoding.EncodeToString(chunk)})
			if s.fc {
				if rem := budget - n; rem > 0 {
					s.addCredit(rem)
				}
			}
		}
		if err != nil {
			c.send(msg{Type: typeTunnelClose, TunnelID: c.tunnelID, StreamID: s.streamID})
			c.markDone(s, true)
			return
		}
	}
}

func (c *Client) markDone(s *stream, read bool) {
	s.stateMu.Lock()
	if read {
		s.readDone = true
	} else {
		s.writeDone = true
	}
	both := s.readDone && s.writeDone
	s.stateMu.Unlock()
	if both {
		s.abort()
		c.unregister(s.streamID)
	}
}

func (c *Client) shutdownStreams() {
	c.mu.Lock()
	all := make([]*stream, 0, len(c.streams))
	for _, s := range c.streams {
		all = append(all, s)
	}
	c.streams = make(map[string]*stream)
	c.mu.Unlock()
	for _, s := range all {
		s.abort()
	}
}

func (c *Client) register(s *stream) {
	c.mu.Lock()
	c.streams[s.streamID] = s
	c.mu.Unlock()
}

func (c *Client) unregister(streamID string) {
	c.mu.Lock()
	delete(c.streams, streamID)
	c.mu.Unlock()
}

func (c *Client) send(m msg) {
	m.Timestamp = time.Now().Unix()
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	c.conn.WriteMessage(websocket.TextMessage, data) //nolint:errcheck
}

func (c *Client) pinger(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.writeMu.Lock()
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			err := c.conn.WriteMessage(websocket.PingMessage, nil)
			c.writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

// wsTunnelURL converts the API HTTP URL into the WSS URL of the tunnel endpoint.
func wsTunnelURL(apiURL, token, tunnelID string) (string, error) {
	apiURL = strings.TrimRight(apiURL, "/")
	apiURL = strings.TrimSuffix(apiURL, "/api/v1")
	u, err := url.Parse(apiURL)
	if err != nil {
		return "", fmt.Errorf("invalid api-url: %w", err)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("unsupported scheme in api-url: %s", u.Scheme)
	}
	u.Path = "/ws/tunnel"
	q := url.Values{}
	q.Set("tunnel", tunnelID)
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func safeClose(ch chan struct{}) {
	defer func() { _ = recover() }()
	close(ch)
}
