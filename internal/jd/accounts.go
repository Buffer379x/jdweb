package jd

import "context"

const (
	pathAccList     = "/accountsV2/listAccounts"
	pathAccAdd      = "/accountsV2/addAccount"
	pathAccSetLogin = "/accountsV2/setUserNameAndPassword"
	pathAccEnable   = "/accountsV2/enableAccounts"
	pathAccDisable  = "/accountsV2/disableAccounts"
	pathAccRemove   = "/accountsV2/removeAccounts"
	pathAccRefresh  = "/accountsV2/refreshAccounts"
	pathAccHosters  = "/accountsV2/listPremiumHoster"
	pathBasicList   = "/accountsV2/listBasicAuth"
	pathBasicAdd    = "/accountsV2/addBasicAuth"
	pathBasicUpdate = "/accountsV2/updateBasicAuth"
	pathBasicRemove = "/accountsV2/removeBasicAuths"
)

// Account is a premium account. JDownloader never returns the password.
// TrafficLeft, TrafficMax and ValidUntil are -1 when unknown.
type Account struct {
	UUID        int64  `json:"uuid"`
	Hostname    string `json:"hostname"`
	Username    string `json:"username"`
	Enabled     bool   `json:"enabled"`
	Valid       bool   `json:"valid"`
	ErrorType   string `json:"errorType"` // for example INVALID, empty without error
	ErrorString string `json:"errorString"`
	TrafficLeft int64  `json:"trafficLeft"`
	TrafficMax  int64  `json:"trafficMax"`
	ValidUntil  int64  `json:"validUntil"` // Unix milliseconds
}

// BasicAuth is a stored HTTP or FTP login for a host mask. The password is
// sent to JDownloader but never returned.
type BasicAuth struct {
	ID            int64         `json:"id"`
	Type          BasicAuthType `json:"type"`
	HostMask      string        `json:"hostmask"`
	Username      string        `json:"username"`
	Password      string        `json:"password,omitempty"`
	Enabled       bool          `json:"enabled"`
	Created       int64         `json:"created"`
	LastValidated int64         `json:"lastValidated"`
}

// Accounts lists all accounts.
func (c *Client) Accounts(ctx context.Context) (out []Account, err error) {
	q := map[string]any{
		"enabled": true, "error": true, "trafficLeft": true, "trafficMax": true,
		"userName": true, "valid": true, "validUntil": true,
	}
	return out, c.Call(ctx, pathAccList, &out, q)
}

// AddAccount adds an account for a hoster. JDownloader checks it in the background.
func (c *Client) AddAccount(ctx context.Context, hoster, username, password string) error {
	return c.Call(ctx, pathAccAdd, nil, hoster, username, password)
}

// SetAccountLogin changes username and password of an account.
func (c *Client) SetAccountLogin(ctx context.Context, id int64, username, password string) error {
	return c.Call(ctx, pathAccSetLogin, nil, id, username, password)
}

// EnableAccounts enables accounts.
func (c *Client) EnableAccounts(ctx context.Context, accountIDs []int64) error {
	return c.Call(ctx, pathAccEnable, nil, ids(accountIDs))
}

// DisableAccounts disables accounts.
func (c *Client) DisableAccounts(ctx context.Context, accountIDs []int64) error {
	return c.Call(ctx, pathAccDisable, nil, ids(accountIDs))
}

// RemoveAccounts removes accounts.
func (c *Client) RemoveAccounts(ctx context.Context, accountIDs []int64) error {
	return c.Call(ctx, pathAccRemove, nil, ids(accountIDs))
}

// RefreshAccounts checks accounts again. force also checks accounts that were checked recently.
func (c *Client) RefreshAccounts(ctx context.Context, accountIDs []int64, force bool) error {
	return c.Call(ctx, pathAccRefresh, nil, ids(accountIDs), force)
}

// PremiumHosters lists the hosters JDownloader has account support for.
func (c *Client) PremiumHosters(ctx context.Context) (out []string, err error) {
	return out, c.Call(ctx, pathAccHosters, &out)
}

// BasicAuths lists the stored basic authentication entries.
func (c *Client) BasicAuths(ctx context.Context) (out []BasicAuth, err error) {
	return out, c.Call(ctx, pathBasicList, &out)
}

// AddBasicAuth adds an entry and returns its id.
func (c *Client) AddBasicAuth(ctx context.Context, t BasicAuthType, hostMask, username, password string) (id int64, err error) {
	return id, c.Call(ctx, pathBasicAdd, &id, t, hostMask, username, password)
}

// UpdateBasicAuth replaces the entry with the same ID.
func (c *Client) UpdateBasicAuth(ctx context.Context, a BasicAuth) error {
	return c.Call(ctx, pathBasicUpdate, nil, a)
}

// RemoveBasicAuths removes entries.
func (c *Client) RemoveBasicAuths(ctx context.Context, authIDs []int64) error {
	return c.Call(ctx, pathBasicRemove, nil, ids(authIDs))
}
