package main

// Gateway request handlers, one per request type. A request with no handler gets an empty reply
// of its type and an "!! unhandled" log line.

import (
	"errors"
	"sync"
)

const (
	gw     = "Catapult.Services.Gateway.Protocol.Client."
	player = "FallGuys.Player.Protocol.Client."
)

func gatewayHandlers() map[string]handler {
	return map[string]handler{
		gw + "AuthenticationRequest":                                     authenticate,
		player + "Profile.GetProfileQuery":                               getProfile,
		player + "Settings.GetAllSettingsQuery":                          getSettings,
		player + "Settings.ConfigureSettingsCommand":                     configureSettings,
		player + "Wallet.GetWalletQuery":                                 getWallet,
		gwMM + "Fn.GetPlayerMatchmakingTokenQuery":                       getMatchmakingToken,
		gwMM + "CreateGameEntryTokenCommand":                             createGameEntryToken,
		"FallGuys.Match.Protocol.Client.MatchStatus.GetMatchStatusQuery": getMatchStatus,
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

func (g *gwSession) acct() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.account
}

func getProfile(g *gwSession, _ map[string]any) (map[string]any, error) {
	a := g.acct()
	name := "OpenPak " + a[:4]
	return map[string]any{
		"Id": a, "PlayerName": name, "Tags": []any{},
		"InternalAccountName": map[string]any{"PlayerName": name},
		"ExternalAccountName": name, "IsBanned": false,
	}, nil
}

// settings are per account and in memory: they only need to survive a session for now.
var (
	settingsMu sync.Mutex
	settings   = map[string]map[string]any{}
)

func getSettings(g *gwSession, _ map[string]any) (map[string]any, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	v := settings[g.acct()]
	if v == nil {
		v = map[string]any{}
	}
	return map[string]any{"Values": v}, nil
}

func configureSettings(g *gwSession, body map[string]any) (map[string]any, error) {
	settingsMu.Lock()
	v := settings[g.acct()]
	if v == nil {
		v = map[string]any{}
		settings[g.acct()] = v
	}
	if in, ok := body["Values"].(map[string]any); ok {
		for k, x := range in {
			v[k] = x
		}
	}
	settingsMu.Unlock()
	return getSettings(g, nil)
}

// getWallet: everything in the store is owned, so currency only needs to look plausible.
func getWallet(_ *gwSession, _ map[string]any) (map[string]any, error) {
	return map[string]any{"Kudos": int64(99999), "Crowns": int64(0), "CrownShards": int64(0),
		"GemsPurchased": int64(0), "GemsGranted": int64(0), "CurrencyAmounts": map[string]any{}}, nil
}
