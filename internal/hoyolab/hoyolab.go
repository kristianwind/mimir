// Package hoyolab reads live account state from HoYoLAB's Battle Chronicle.
//
// This is the only source that makes Mimir proactive rather than reactive:
// resin, daily commissions, expeditions and Abyss progress are not in Enka and
// not in a GOOD file. "You will cap resin in 40 minutes and today's domain
// matches your top goal" needs this endpoint and nothing else provides it.
//
// Three things to know before relying on it:
//
//   - It is unofficial. There is no contract, and HoYoverse can change or
//     close it without notice. Nothing else in Mimir depends on it.
//   - It requires the user to enable Real-Time Notes themselves in the
//     HoYoLAB app. Mimir cannot do this for them.
//   - Its cookies are full account credentials. They are stored encrypted
//     (see internal/secrets), never logged and never returned by the API.
//
// The request signature ("DS" header) is salted with a value that ships in
// HoYoLAB's own web client and changes with its releases. It is configuration
// here rather than a constant in the source, so a rotation is a settings
// change instead of a release.
package hoyolab

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config carries the values that HoYoLAB rotates.
type Config struct {
	// BaseURL is the regional Battle Chronicle host.
	BaseURL string
	// Salt is the DS salt from the current web client release.
	Salt string
	// ClientVersion is sent as x-rpc-app_version and must match the salt.
	ClientVersion string
	// ClientType is sent as x-rpc-client_type ("5" for the web client).
	ClientType string
	// Language is sent as x-rpc-language.
	Language string
}

// Client talks to the Battle Chronicle.
type Client struct {
	Config Config
	HTTP   *http.Client
	// Now and Nonce are injectable so the signature is testable.
	Now   func() time.Time
	Nonce func() string
}

// New returns a client with sensible transport defaults. The caller supplies
// the config because none of it can be safely hardcoded.
func New(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://bbs-api-os.hoyolab.com"
	}
	if cfg.ClientType == "" {
		cfg.ClientType = "5"
	}
	if cfg.Language == "" {
		cfg.Language = "en-us"
	}
	return &Client{
		Config: cfg,
		HTTP:   &http.Client{Timeout: 20 * time.Second},
		Now:    time.Now,
		Nonce:  randomNonce,
	}
}

// DS builds the request signature: md5 of the timestamp, a random string and
// the salt, prefixed by the first two.
func (c *Client) DS() string {
	t := strconv.FormatInt(c.Now().Unix(), 10)
	r := c.Nonce()
	sum := md5.Sum([]byte("salt=" + c.Config.Salt + "&t=" + t + "&r=" + r))
	return t + "," + r + "," + hex.EncodeToString(sum[:])
}

func randomNonce() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = alphabet[rand.IntN(len(alphabet))]
	}
	return string(b)
}

// Notes is the Real-Time Notes payload: what Mimir schedules against.
type Notes struct {
	CurrentResin           int    `json:"current_resin"`
	MaxResin               int    `json:"max_resin"`
	ResinRecoveryTime      string `json:"resin_recovery_time"`
	FinishedTaskNum        int    `json:"finished_task_num"`
	TotalTaskNum           int    `json:"total_task_num"`
	RemainResinDiscountNum int    `json:"remain_resin_discount_num"`
	ResinDiscountNumLimit  int    `json:"resin_discount_num_limit"`
	CurrentExpeditionNum   int    `json:"current_expedition_num"`
	MaxExpeditionNum       int    `json:"max_expedition_num"`
	Expeditions            []struct {
		Status       string `json:"status"`
		RemainedTime string `json:"remained_time"`
	} `json:"expeditions"`
	Transformer struct {
		Obtained     bool `json:"obtained"`
		RecoveryTime struct {
			Days    int  `json:"Day"`
			Hours   int  `json:"Hour"`
			Minutes int  `json:"Minute"`
			Reached bool `json:"reached"`
		} `json:"recovery_time"`
	} `json:"transformer"`
}

// ResinFullAt returns when resin will cap, or the zero time if it already has.
func (n Notes) ResinFullAt(now time.Time) time.Time {
	secs, err := strconv.Atoi(n.ResinRecoveryTime)
	if err != nil || secs <= 0 {
		return time.Time{}
	}
	return now.Add(time.Duration(secs) * time.Second)
}

type envelope struct {
	Retcode int             `json:"retcode"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// FetchNotes reads Real-Time Notes for a UID.
//
// cookies is the raw Cookie header value the user pasted; it is passed
// straight through and never parsed, so Mimir never has to hold the individual
// token values in a struct that might end up in a log line.
func (c *Client) FetchNotes(ctx context.Context, uid, server, cookies string) (*Notes, error) {
	if c.Config.Salt == "" {
		return nil, fmt.Errorf("hoyolab: no DS salt configured; the integration is disabled")
	}

	q := url.Values{"role_id": {uid}, "server": {server}}
	endpoint := strings.TrimRight(c.Config.BaseURL, "/") +
		"/game_record/genshin/api/dailyNote?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", cookies)
	req.Header.Set("DS", c.DS())
	req.Header.Set("x-rpc-app_version", c.Config.ClientVersion)
	req.Header.Set("x-rpc-client_type", c.Config.ClientType)
	req.Header.Set("x-rpc-language", c.Config.Language)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hoyolab: fetch notes: %w", err)
	}
	defer resp.Body.Close()

	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("hoyolab: decode: %w", err)
	}
	if env.Retcode != 0 {
		return nil, retcodeError(env.Retcode, env.Message)
	}

	var notes Notes
	if err := json.Unmarshal(env.Data, &notes); err != nil {
		return nil, fmt.Errorf("hoyolab: decode notes: %w", err)
	}
	return &notes, nil
}

// retcodeError turns HoYoLAB's numeric codes into messages that say what the
// user has to do. The API answers HTTP 200 for every failure, so this is the
// only place the difference between "log in again" and "turn on Real-Time
// Notes" can be made.
func retcodeError(code int, message string) error {
	switch code {
	case 10001:
		return fmt.Errorf("hoyolab: cookies are invalid or expired (retcode 10001) — log in to HoYoLAB again and paste fresh cookies")
	case 10102:
		return fmt.Errorf("hoyolab: this account's data is private (retcode 10102) — enable Real-Time Notes in the HoYoLAB app")
	case 10103:
		return fmt.Errorf("hoyolab: the cookies do not belong to this UID (retcode 10103)")
	case 1034:
		return fmt.Errorf("hoyolab: blocked by a challenge (retcode 1034) — open HoYoLAB in a browser once, then retry")
	default:
		return fmt.Errorf("hoyolab: request failed (retcode %d): %s", code, message)
	}
}

// Server maps a UID to the server string the API expects.
func Server(uid string) string {
	if uid == "" {
		return ""
	}
	switch uid[0] {
	case '6':
		return "os_usa"
	case '7':
		return "os_euro"
	case '8':
		return "os_asia"
	case '9':
		return "os_cht"
	default:
		return "cn_gf01"
	}
}
