package main

// The gateway socket: wss://gateway.fallguys.oncatapult.com/ws. Every frame is one protobuf-net
// Catapult Message. A request is Message<Q> with an OperationHeader {RequestId, Request}. The
// reply is Result<R> with the same RequestId and Reply, where R is the reply type the client's
// request sender expects (internal/catapult/rpc.json). The first request is
// AuthenticationRequest, which carries the session token from /api/v1/login.

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"openpak/fall-guys/internal/catapult"
)

// handler answers one request body with a reply body.
type handler func(g *gwSession, body map[string]any) (map[string]any, error)

type gwSession struct {
	s       *server
	ws      *wsConn
	account string // set by AuthenticationRequest
	mu      sync.Mutex
}

// errNoReply marks a request whose reply the handler sends itself, or none at all.
var errNoReply = errors.New("no reply")

func (s *server) gateway(w http.ResponseWriter, r *http.Request) {
	ws, err := wsUpgrade(w, r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	g := &gwSession{s: s, ws: ws}
	log.Printf("[gateway] connected from %s", strings.Split(r.RemoteAddr, ":")[0])
	defer ws.close()
	for {
		p, err := ws.read()
		if err != nil {
			log.Printf("[gateway] closed: %v", err)
			return
		}
		g.handle(p)
	}
}

func (g *gwSession) handle(p []byte) {
	msg, err := g.s.schema.Decode(catapult.MessageType, p)
	if err != nil {
		log.Printf("[gateway] !! undecodable message (%d bytes): %v", len(p), err)
		return
	}
	typ, _ := msg["$type"].(string)
	body, _ := msg["Body"].(map[string]any)
	reqID, op := operation(msg)
	name := strings.TrimSuffix(strings.TrimPrefix(typ, "Catapult.Protocol.Message`1<"), ">")
	if op != catapult.OpRequest {
		log.Printf("[gateway] one-way %s keys=%s", short(name), keys(body))
		if h, ok := g.s.handlers[name]; ok {
			_, _ = h(g, body)
		}
		return
	}
	replyType, known := g.s.replies[name]
	h, ok := g.s.handlers[name]
	if !ok {
		// Unknown to us: answer with an empty reply of the right type, which reads as "nothing
		// here" (empty lists, zero counts), and log it as the next handler to write.
		log.Printf("[gateway] !! unhandled request %s keys=%s reply=%s (sent empty)", name, keys(body), short(replyType))
		if known && replyType != "" {
			g.send(catapult.ResultOf(replyType), reqID, catapult.OpReply, map[string]any{})
		}
		return
	}
	resp, err := h(g, body)
	if errors.Is(err, errNoReply) {
		return
	}
	if err != nil {
		log.Printf("[gateway] %s failed: %v", short(name), err)
		g.sendError(reqID, replyType, 500, err.Error())
		return
	}
	log.Printf("[gateway] %s -> %s", short(name), short(replyType))
	g.send(catapult.ResultOf(replyType), reqID, catapult.OpReply, resp)
}

// operation returns the request id and operation type from the message headers.
func operation(msg map[string]any) (string, int64) {
	hs, _ := msg["Headers"].([]any)
	for _, h := range hs {
		m, _ := h.(map[string]any)
		if m["$type"] == catapult.OperationHeader {
			id, _ := m["RequestId"].(string)
			t, _ := m["Type"].(int64)
			return id, t
		}
	}
	return "", catapult.OpOneWay
}

func (g *gwSession) send(typ, reqID string, op int, body map[string]any) {
	g.sendMsg(typ, reqID, op, body, nil)
}

func (g *gwSession) sendMsg(typ, reqID string, op int, body, errDesc map[string]any) {
	msg := map[string]any{"$type": typ}
	if errDesc != nil {
		msg["Error"] = errDesc
	}
	if body != nil {
		msg["Body"] = body
	}
	if reqID != "" {
		msg["Headers"] = []any{map[string]any{"$type": catapult.OperationHeader, "RequestId": reqID, "Type": int64(op)}}
	}
	p, err := g.s.schema.Encode(catapult.MessageType, msg)
	if err != nil {
		log.Printf("[gateway] !! cannot encode %s: %v", short(typ), err)
		return
	}
	if err := g.ws.write(p); err != nil {
		log.Printf("[gateway] write: %v", err)
	}
}

// push sends a one-way Message<T> (notifications the client subscribes to).
func (g *gwSession) push(bodyType string, body map[string]any) {
	g.send(catapult.MessageOf(bodyType), newGUID(), catapult.OpOneWay, body)
}

func (g *gwSession) sendError(reqID, replyType string, status int, reason string) {
	g.sendMsg(catapult.ResultOf(replyType), reqID, catapult.OpReply, nil,
		map[string]any{"StatusCode": int64(status), "ErrorString": reason, "Message": reason})
}

func short(t string) string {
	if i := strings.LastIndexByte(t, '.'); i >= 0 && !strings.Contains(t[i:], ">") {
		return t[i+1:]
	}
	return t
}

func newGUID() string {
	b := []byte(randomHex(32))
	return fmt.Sprintf("%s-%s-4%s-a%s-%s", b[0:8], b[8:12], b[13:16], b[17:20], b[20:32])
}

func nowUTC() time.Time { return time.Now().UTC() }
