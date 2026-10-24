package catapult

import (
	"testing"
	"time"
)

func TestMessageRoundTrip(t *testing.T) {
	s, err := LoadSchema()
	if err != nil {
		t.Fatal(err)
	}
	const q = "Catapult.Protocol.Message`1<Catapult.Services.Gateway.Protocol.Client.AuthenticationRequest>"
	in := map[string]any{
		"$type": q,
		"Headers": []any{
			map[string]any{"$type": "Catapult.Protocol.Headers.OperationHeader",
				"RequestId": "0f8fad5b-d9cb-469f-a165-70867728950e", "Type": int64(1)},
		},
		"Body": map[string]any{"Token": "tok", "ProtocolVersion": "1", "ContinueSession": true},
	}
	b, err := s.Encode("Catapult.Protocol.Message", in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Decode("Catapult.Protocol.Message", b)
	if err != nil {
		t.Fatal(err)
	}
	if out["$type"] != q {
		t.Fatalf("type %v", out["$type"])
	}
	body := out["Body"].(map[string]any)
	if body["Token"] != "tok" || body["ContinueSession"] != true {
		t.Fatalf("body %v", body)
	}
	h := out["Headers"].([]any)[0].(map[string]any)
	if h["RequestId"] != "0f8fad5b-d9cb-469f-a165-70867728950e" || h["Type"] != int64(1) {
		t.Fatalf("header %v", h)
	}
}

func TestDateTimeAndDictionary(t *testing.T) {
	when := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	got, err := decodeDateTime(encodeDateTime(when))
	if err != nil || !got.Equal(when) {
		t.Fatalf("datetime %v %v", got, err)
	}
	if k, a := splitGeneric("System.Collections.Generic.Dictionary`2<string,System.Collections.Generic.List`1<int>>"); k != "System.Collections.Generic.Dictionary`2" || len(a) != 2 || a[1] != "System.Collections.Generic.List`1<int>" {
		t.Fatalf("split %v %v", k, a)
	}
}
