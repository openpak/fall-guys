package catapult

import (
	_ "embed"
	"encoding/json"
)

// rpc.json: each request type the client sends and the reply type it waits for ("" for a
// one-way message), from the generic instantiations of its request sender.
//
//go:embed rpc.json
var rpcJSON []byte

// Replies maps a request body type to its reply body type.
func Replies() (map[string]string, error) {
	m := map[string]string{}
	return m, json.Unmarshal(rpcJSON, &m)
}

// Message type names.
const (
	MessageType                  = "Catapult.Protocol.Message"
	OperationHeader              = "Catapult.Protocol.Headers.OperationHeader"
	OpOneWay, OpRequest, OpReply = 0, 1, 2
)

func MessageOf(body string) string { return "Catapult.Protocol.Message`1<" + body + ">" }
func ResultOf(body string) string  { return "Catapult.Protocol.Result`1<" + body + ">" }
