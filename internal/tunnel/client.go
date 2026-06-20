// Package tunnel implementa los dos extremos de un túnel del CLI sobre un WebSocket al
// backend, con el mismo manejo de half-close, drenado y reset ante saturación que el
// agente:
//   - rol source (Tipo 1, local): abre un listener TCP en la máquina del usuario y
//     multiplexa cada conexión hacia el agente destino, que hace el dial.
//   - rol dest (Tipo 2 reverse): el agente source abre el listener público en el VPS y
//     el backend relaya cada conexión entrante al CLI, que hace el dial a un servicio
//     local del usuario (caso ngrok/webhooks). Es el espejo del rol dest del agente.
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

// Tipos de mensaje del protocolo de túneles (sincronizados con el backend).
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
	// windowSize: créditos iniciales (bytes en vuelo) por stream y dirección. Mismo
	// control de flujo que el agente: el emisor no lee más allá de la ventana sin
	// crédito; el receptor concede crédito (tunnel_window) al drenar.
	windowSize = 256 * 1024
)

type msg struct {
	Type       string `json:"type"`
	TunnelID   string `json:"tunnel_id,omitempty"`
	StreamID   string `json:"stream_id,omitempty"`
	Data       string `json:"data,omitempty"`
	Error      string `json:"error,omitempty"`
	OK         bool   `json:"ok,omitempty"`
	FC         bool   `json:"fc,omitempty"`         // capacidad de control de flujo (open/open_ack)
	Host       string `json:"host,omitempty"`       // backend→dest: a dónde hacer el dial
	Port       int    `json:"port,omitempty"`       // backend→dest
	LocalPort  int    `json:"local_port,omitempty"` // tunnel_ready
	RemoteHost string `json:"remote_host,omitempty"`
	RemotePort int    `json:"remote_port,omitempty"`
	Bytes      int    `json:"bytes,omitempty"` // tunnel_window: crédito concedido
	Timestamp  int64  `json:"timestamp,omitempty"`
}

// role distingue el papel del CLI dentro del túnel.
type role int

const (
	roleSource role = iota // Tipo 1: el CLI escucha localmente
	roleDest               // Tipo 2 reverse: el CLI hace el dial al servicio local
)

// Client mantiene la sesión de un túnel del CLI.
type Client struct {
	tunnelID string
	role     role
	// localAddr (source): dirección donde escuchar localmente.
	// dialAddr  (dest):   dirección del servicio local a la que hacer el dial.
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

	fc         bool // ambos extremos soportan créditos → gating activo
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

// initFlow inicializa el control de flujo con la ventana completa.
func (s *stream) initFlow() {
	s.creditCond = sync.NewCond(&s.creditMu)
	s.sendCredit = windowSize
}

// takeCredit bloquea hasta que haya crédito (o el stream termine) y reserva hasta
// `max` bytes. Devuelve 0 si el stream está cerrado.
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

// New crea un cliente rol source (Tipo 1): escucha en localAddr y reenvía al agente.
// apiURL es la URL HTTP de la API (con o sin /api/v1).
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

// NewDest crea un cliente rol dest (Tipo 2 reverse): por cada conexión que el backend
// relaya, hace el dial al servicio local en dialAddr.
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

// dialTunnel abre el WebSocket autenticado al endpoint de túneles del backend.
func dialTunnel(apiURL, token, tunnelID string) (*websocket.Conn, error) {
	wsURL, err := wsTunnelURL(apiURL, token, tunnelID)
	if err != nil {
		return nil, err
	}
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, resp, err := dialer.Dial(wsURL, http.Header{})
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("no se pudo abrir la sesión (HTTP %d): %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("no se pudo conectar al backend: %w", err)
	}
	return conn, nil
}

// Run abre el listener local y relaya hasta que ctx se cancela, el backend cierra la
// sesión (tunnel_stop) o el WebSocket cae. Llama a onReady cuando la sesión queda lista.
func (c *Client) Run(ctx context.Context, onReady func(localPort, remotePort int, remoteHost string)) error {
	defer c.conn.Close()

	c.conn.SetReadLimit(9 << 20)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	go c.pinger(ctx)

	// Esperar tunnel_ready antes de abrir el listener.
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
		return fmt.Errorf("el backend no confirmó la sesión a tiempo")
	}

	// Rol source: abrir el listener local y aceptar conexiones. Rol dest: no hay
	// listener local — el dial lo dispara cada tunnel_open que llega por readLoop.
	if c.role == roleSource {
		ln, err := net.Listen("tcp", c.localAddr)
		if err != nil {
			return fmt.Errorf("no se pudo abrir el puerto local %s: %w", c.localAddr, err)
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

// openDest (rol dest) hace el dial al servicio local y, si tiene éxito, arranca el relay.
// Prioriza dialAddr (--to del usuario); si está vacío usa el host:port del tunnel_open
// (los valores remote_host/remote_port guardados en el túnel). peerFC indica si el
// source soporta control de flujo (el gating se activa sólo si ambos lo soportan).
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
			c.logf("dial a %s falló: %v", addr, err)
			c.send(msg{Type: typeTunnelOpenAck, TunnelID: c.tunnelID, StreamID: streamID, OK: false, Error: err.Error()})
			c.unregister(streamID)
			return
		}
		s.conn = conn
		// FC: peerFC hace eco de la capacidad negociada (fallback limpio si el flag no
		// llegó por un backend antiguo).
		c.send(msg{Type: typeTunnelOpenAck, TunnelID: c.tunnelID, StreamID: streamID, OK: true, FC: peerFC})
		c.relay(s)
	}()
}

func (c *Client) readLoop(readErr chan<- error) {
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			readErr <- nil // cierre normal del WS: fin de la sesión
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
			// Sólo el rol dest recibe tunnel_open: hace el dial al servicio local.
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
			readErr <- nil // el backend cerró el túnel
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
		s.fc = peerFC // se fija antes de cerrar ready (el relay arranca después)
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
		return // stream cerrado en esta dirección: no enviar (evita panic)
	}
	select {
	case <-s.done:
	case s.inbox <- b:
	default:
		// Buffer saturado: resetear el stream (sin descartar bytes a media res).
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

// closeStream cierra la dirección peer→local (half-close). NO elimina el stream del
// mapa: la dirección local→peer puede seguir activa y necesita recibir créditos. El
// stream se elimina vía markDone cuando ambas direcciones terminan.
func (c *Client) closeStream(streamID string) {
	c.mu.Lock()
	s := c.streams[streamID]
	c.mu.Unlock()
	if s != nil {
		s.closeInbox()
	}
}

func (c *Client) relay(s *stream) {
	// Escritor (peer→local): inbox → conn; ante EOF ordenado, drenar y half-close.
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
				// Conceder crédito al emisor opuesto: ya drenamos len(b).
				if s.fc {
					c.send(msg{Type: typeTunnelWindow, TunnelID: c.tunnelID, StreamID: s.streamID, Bytes: len(b)})
				}
			}
		}
	}()

	// Lector (local→peer): conn → tunnel_data. Con control de flujo (s.fc) espera crédito
	// antes de leer → backpressure al origen si el receptor opuesto va lento. Sin fc (peer
	// antiguo) lee libremente. Al ver EOF señala el fin de la dirección.
	buf := make([]byte, relayBuf)
	for {
		budget := len(buf)
		if s.fc {
			budget = s.takeCredit(len(buf))
			if budget == 0 {
				return // stream cerrado mientras esperaba crédito
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

// wsTunnelURL convierte la URL HTTP de la API en la URL WSS del endpoint de túneles.
func wsTunnelURL(apiURL, token, tunnelID string) (string, error) {
	apiURL = strings.TrimRight(apiURL, "/")
	apiURL = strings.TrimSuffix(apiURL, "/api/v1")
	u, err := url.Parse(apiURL)
	if err != nil {
		return "", fmt.Errorf("api-url inválida: %w", err)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("esquema no soportado en api-url: %s", u.Scheme)
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
