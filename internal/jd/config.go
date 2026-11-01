package jd

import (
	"context"
	"encoding/json"
)

const (
	pathConfigQuery = "/config/query"
	pathConfigGet   = "/config/get"
	pathConfigSet   = "/config/set"
	pathConfigReset = "/config/reset"
)

// generalSettings is the config interface that holds the speed limit.
const generalSettings = "org.jdownloader.settings.GeneralSettings"

// ConfigEntry is one setting of JDownloader, from the config or a plugin.
// Value and DefaultValue are JSON of the kind AbstractType names.
type ConfigEntry struct {
	InterfaceName string          `json:"interfaceName"`
	Storage       string          `json:"storage"` // empty for the general config, the plugin name for plugins
	Key           string          `json:"key"`
	Type          string          `json:"type"`         // Java type
	AbstractType  string          `json:"abstractType"` // BOOLEAN, INT, LONG, STRING, ENUM, ...
	Docs          string          `json:"docs"`
	Value         json.RawMessage `json:"value"`
	DefaultValue  json.RawMessage `json:"defaultValue"`
	EnumLabel     string          `json:"enumLabel"`
	EnumOptions   [][]string      `json:"enumOptions"` // pairs of name and label
}

// ConfigQuery selects settings. Pattern is a regular expression on interface
// name and key.
type ConfigQuery struct {
	Pattern           string `json:"pattern,omitempty"`
	ConfigInterface   string `json:"configInterface,omitempty"`
	Description       bool   `json:"description"`
	Values            bool   `json:"values"`
	DefaultValues     bool   `json:"defaultValues"`
	EnumInfo          bool   `json:"enumInfo"`
	IncludeExtensions bool   `json:"includeExtensions,omitempty"`
}

// storage turns "" into null, which is how JDownloader names the general config.
func storage(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ConfigQuery returns settings of the general config and the extensions.
func (c *Client) ConfigQuery(ctx context.Context, q ConfigQuery) (out []ConfigEntry, err error) {
	return out, c.Call(ctx, pathConfigQuery, &out, q)
}

// ConfigGet returns one value as JSON, or nil if the key does not exist.
func (c *Client) ConfigGet(ctx context.Context, iface, stor, key string) (v json.RawMessage, err error) {
	return v, c.Call(ctx, pathConfigGet, &v, iface, storage(stor), key)
}

// ConfigSet sets one value. JDownloader refuses a value of the wrong type
// and an unknown key with false, which is ErrRejected here.
func (c *Client) ConfigSet(ctx context.Context, iface, stor, key string, value any) error {
	return boolResult(ctx, c, pathConfigSet, iface, storage(stor), key, value)
}

// ConfigReset sets one value back to its default. An unknown key is ErrRejected.
func (c *Client) ConfigReset(ctx context.Context, iface, stor, key string) error {
	return boolResult(ctx, c, pathConfigReset, iface, storage(stor), key)
}

// boolResult calls a method that answers true on success and false on refusal.
func boolResult(ctx context.Context, c *Client, path string, params ...any) error {
	var ok bool
	if err := c.Call(ctx, path, &ok, params...); err != nil {
		return err
	}
	if !ok {
		return ErrRejected
	}
	return nil
}

// SetSpeedLimit sets the speed limit in bytes per second and switches it on or
// off. With bytesPerSecond <= 0 only the switch changes.
func (c *Client) SetSpeedLimit(ctx context.Context, enabled bool, bytesPerSecond int64) error {
	if bytesPerSecond > 0 {
		if err := c.ConfigSet(ctx, generalSettings, "", "DownloadSpeedLimit", bytesPerSecond); err != nil {
			return err
		}
	}
	return c.ConfigSet(ctx, generalSettings, "", "DownloadSpeedLimitEnabled", enabled)
}

// SpeedLimit returns the configured speed limit in bytes per second and
// whether it is switched on.
func (c *Client) SpeedLimit(ctx context.Context) (enabled bool, bytesPerSecond int64, err error) {
	raw, err := c.ConfigGet(ctx, generalSettings, "", "DownloadSpeedLimitEnabled")
	if err != nil {
		return false, 0, err
	}
	_ = json.Unmarshal(raw, &enabled)
	raw, err = c.ConfigGet(ctx, generalSettings, "", "DownloadSpeedLimit")
	if err != nil {
		return false, 0, err
	}
	_ = json.Unmarshal(raw, &bytesPerSecond)
	return enabled, bytesPerSecond, nil
}
