package main

// Live player counts, for projects that publish them. Today only OpenCE
// does: its official master server lists every public server with its
// player count, signed with the master's Ed25519 key (OpenCE's
// docs/master_protocol.md, "The list"). The launcher checks that signature
// and the list's age exactly as OpenCE's own server browser does, and only
// then adds the numbers up. It reads the list at most once a minute, and
// only while the launcher's window is open.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Online struct {
	Players int       `json:"players"`
	Servers int       `json:"servers"`
	At      time.Time `json:"at"`
}

// OpenCE's official master and its list-signing key (as OpenCE's own
// master_list.c and servers.js pin them). Vars only so tests can use a
// test key.
var (
	openceMasterURL = "https://master.opencommunityedition.com/v1/servers"
	openceMasterKey = "89f4eda71229f11f3538679f49ef7fb0c2b407b380093f7f4fe02105b92225aa"
)

const (
	onlineRefresh = 60 * time.Second
	onlineKeep    = 10 * time.Minute // show the last good count this long
	listMaxAge    = 300              // seconds, as OpenCE's clients
	listMaxBody   = 8 << 20
)

var (
	onlineMu      sync.Mutex
	onlineCache   = map[string]Online{}
	onlineTried   = map[string]time.Time{}
	onlineLoading = map[string]bool{}
	onlineSources = map[string]func() (Online, error){"ce": fetchOpenCEOnline}
)

// getOnline returns the latest count for a game (nil when there is none, or
// it's too old), and refreshes it in the background when due.
func getOnline(id string) *Online {
	fetch := onlineSources[id]
	if fetch == nil {
		return nil
	}
	onlineMu.Lock()
	defer onlineMu.Unlock()
	if time.Since(onlineTried[id]) > onlineRefresh && !onlineLoading[id] {
		onlineLoading[id] = true
		onlineTried[id] = time.Now()
		go func() {
			o, err := fetch()
			onlineMu.Lock()
			defer onlineMu.Unlock()
			onlineLoading[id] = false
			if err != nil {
				log.Printf("player count for %s: %v", id, err)
				return
			}
			onlineCache[id] = o
		}()
	}
	o, ok := onlineCache[id]
	if !ok || time.Since(o.At) > onlineKeep {
		return nil
	}
	return &o
}

var signatureRe = regexp.MustCompile(`^ed25519=([A-Za-z0-9+/]+={0,2})$`)

func fetchOpenCEOnline() (Online, error) {
	req, _ := http.NewRequest("GET", openceMasterURL, nil)
	req.Header.Set("User-Agent", "HaloModdingCommunityCollection/"+appVersion)
	c := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("redirects not followed")
	}}
	resp, err := c.Do(req)
	if err != nil {
		return Online{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Online{}, fmt.Errorf("master answered %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, listMaxBody+1))
	if err != nil {
		return Online{}, err
	}
	if len(body) > listMaxBody {
		return Online{}, errors.New("list too large")
	}
	return readOpenCEList(body, resp.Header, time.Now())
}

// readOpenCEList checks a master's list and counts its players.
func readOpenCEList(body []byte, h http.Header, now time.Time) (Online, error) {
	key, err := hex.DecodeString(openceMasterKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return Online{}, errors.New("bad master key")
	}
	sum := sha256.Sum256(key)
	keyID := hex.EncodeToString(sum[:8])
	m := signatureRe.FindStringSubmatch(strings.TrimSpace(h.Get("Signature")))
	if m == nil {
		return Online{}, errors.New("no signature")
	}
	if strings.TrimSpace(h.Get("Signature-Key")) != keyID {
		return Online{}, errors.New("signed with another key")
	}
	sig, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil || !ed25519.Verify(ed25519.PublicKey(key), body, sig) {
		return Online{}, errors.New("bad signature")
	}
	var list struct {
		Protocol int `json:"protocol"`
		Master   struct {
			KeyID string `json:"key_id"`
			Time  int64  `json:"time"`
		} `json:"master"`
		Servers []struct {
			ID      string `json:"id"`
			Players int    `json:"players"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return Online{}, errors.New("not JSON")
	}
	if list.Protocol != 1 || list.Master.KeyID != keyID {
		return Online{}, errors.New("not a list from this master")
	}
	if age := now.Unix() - list.Master.Time; age > listMaxAge || age < -listMaxAge {
		return Online{}, errors.New("list too old, or the PC's clock is off")
	}
	o := Online{At: now}
	seen := map[string]bool{}
	for i, s := range list.Servers {
		if i >= 512 || s.ID == "" || seen[s.ID] || s.Players < 0 || s.Players > 255 {
			continue
		}
		seen[s.ID] = true
		o.Servers++
		o.Players += s.Players
	}
	return o, nil
}
