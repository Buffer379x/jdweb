package jd

import (
	"context"
	"encoding/json"
)

const (
	pathPluginList  = "/plugins/list"
	pathPluginQuery = "/plugins/query"
	pathPluginSet   = "/plugins/set"
	pathPluginReset = "/plugins/reset"
)

// Plugin is a hoster or decrypter plugin.
type Plugin struct {
	DisplayName string `json:"displayName"`
	ClassName   string `json:"className"`
	Pattern     string `json:"pattern"` // regular expression of the urls it handles
	Version     string `json:"version"`
}

// Plugins lists the plugins.
func (c *Client) Plugins(ctx context.Context) (out []Plugin, err error) {
	q := struct {
		Pattern bool `json:"pattern"`
		Version bool `json:"version"`
	}{true, true}
	return out, c.Call(ctx, pathPluginList, &out, q)
}

// PluginSettings returns plugin settings. In the entries Storage is the
// display name of the plugin.
func (c *Client) PluginSettings(ctx context.Context, q ConfigQuery) (out []ConfigEntry, err error) {
	return out, c.Call(ctx, pathPluginQuery, &out, q)
}

// PluginSet sets one plugin setting. A refused value is ErrRejected.
func (c *Client) PluginSet(ctx context.Context, iface, displayName, key string, value any) error {
	return boolResult(ctx, c, pathPluginSet, iface, displayName, key, value)
}

// PluginReset sets one plugin setting back to its default.
func (c *Client) PluginReset(ctx context.Context, iface, displayName, key string) error {
	return boolResult(ctx, c, pathPluginReset, iface, displayName, key)
}

// PluginValue returns the value of one plugin setting as JSON, or nil if the
// plugin has no such key. config/get cannot do this: it does not know the
// storage of a plugin and answers an empty body. plugins/query with the
// config interface does return the stored value.
func (c *Client) PluginValue(ctx context.Context, iface, displayName, key string) (json.RawMessage, error) {
	entries, err := c.PluginSettings(ctx, ConfigQuery{ConfigInterface: iface, Values: true})
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Storage == displayName && e.Key == key {
			return e.Value, nil
		}
	}
	return nil, nil
}
