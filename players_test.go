package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type testMaster struct {
	pub   ed25519.PublicKey
	priv  ed25519.PrivateKey
	keyID string
}

func newTestMaster(t *testing.T) *testMaster {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(pub)
	old := openceMasterKey
	openceMasterKey = hex.EncodeToString(pub)
	t.Cleanup(func() { openceMasterKey = old })
	return &testMaster{pub, priv, hex.EncodeToString(sum[:8])}
}

func (m *testMaster) list(at int64, servers string) []byte {
	return []byte(fmt.Sprintf(`{"protocol":1,"master":{"name":"OpenCE Master 1","key_id":"%s","time":%d},"servers":[%s]}`, m.keyID, at, servers))
}

func (m *testMaster) headers(body []byte, priv ed25519.PrivateKey, keyID string) http.Header {
	h := http.Header{}
	h.Set("Signature", "ed25519="+base64.StdEncoding.EncodeToString(ed25519.Sign(priv, body)))
	h.Set("Signature-Key", keyID)
	return h
}

const threeServers = `{"id":"a1","players":5,"max_players":16},{"id":"b2","players":0,"max_players":16},{"id":"c3","players":11,"max_players":32},{"id":"a1","players":5}`

func TestOpenCEListCounted(t *testing.T) {
	m := newTestMaster(t)
	now := time.Now()
	body := m.list(now.Unix()-10, threeServers)
	o, err := readOpenCEList(body, m.headers(body, m.priv, m.keyID), now)
	if err != nil {
		t.Fatal(err)
	}
	if o.Players != 16 || o.Servers != 3 { // the repeated id counts once
		t.Fatalf("got %d players on %d servers", o.Players, o.Servers)
	}
}

func TestOpenCEListRefused(t *testing.T) {
	m := newTestMaster(t)
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now()
	good := m.list(now.Unix(), threeServers)
	tampered := []byte(strings.Replace(string(good), `"players":5`, `"players":250`, 1))
	old := m.list(now.Unix()-301, threeServers)
	for name, c := range map[string]struct {
		body []byte
		h    http.Header
	}{
		"forged signature": {good, m.headers(good, otherPriv, m.keyID)},
		"other key id":     {good, m.headers(good, m.priv, "0000000000000000")},
		"tampered body":    {tampered, m.headers(good, m.priv, m.keyID)},
		"too old":          {old, m.headers(old, m.priv, m.keyID)},
		"no signature":     {good, http.Header{}},
	} {
		if _, err := readOpenCEList(c.body, c.h, now); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestOpenCEOnlineFetched(t *testing.T) {
	m := newTestMaster(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := m.list(time.Now().Unix(), threeServers)
		for k, v := range m.headers(body, m.priv, m.keyID) {
			w.Header()[k] = v
		}
		w.Write(body)
	}))
	defer srv.Close()
	oldURL := openceMasterURL
	openceMasterURL = srv.URL + "/v1/servers"
	t.Cleanup(func() {
		openceMasterURL = oldURL
		onlineMu.Lock()
		delete(onlineCache, "ce")
		delete(onlineTried, "ce")
		onlineMu.Unlock()
	})
	var o *Online
	for i := 0; i < 50 && o == nil; i++ {
		o = getOnline("ce")
		time.Sleep(20 * time.Millisecond)
	}
	if o == nil || o.Players != 16 {
		t.Fatalf("got %+v", o)
	}
	if getOnline("h3") != nil {
		t.Fatal("Halo 3 has no public count")
	}
}
