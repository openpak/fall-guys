package main

// Gateway request handlers, one per request type. Unlisted requests are logged as
// "!! unhandled" with their reply type and answered with an error result.

import "errors"

const gw = "Catapult.Services.Gateway.Protocol.Client."

func gatewayHandlers() map[string]handler {
	return map[string]handler{
		gw + "AuthenticationRequest": authenticate,
	}
}

func authenticate(g *gwSession, body map[string]any) (map[string]any, error) {
	tok, _ := body["Token"].(string)
	account, ok := g.s.checkSession(tok)
	if !ok {
		return nil, errors.New("session token not valid")
	}
	g.mu.Lock()
	g.account = account
	g.mu.Unlock()
	return map[string]any{"UserId": account}, nil
}
