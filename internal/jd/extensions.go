package jd

import "context"

const (
	pathExtensionList    = "/extensions/list"
	pathExtensionInstall = "/extensions/install"
	pathExtensionEnable  = "/extensions/setEnabled"
)

// Extension is an extension of JDownloader. ID is what the other calls take.
type Extension struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Enabled         bool   `json:"enabled"`
	Installed       bool   `json:"installed"`
	IconKey         string `json:"iconKey"`
	ConfigInterface string `json:"configInterface"`
}

// Extensions lists the installed and the available extensions.
func (c *Client) Extensions(ctx context.Context) (out []Extension, err error) {
	q := map[string]any{
		"name": true, "description": true, "enabled": true, "installed": true,
		"iconKey": true, "configInterface": true,
	}
	return out, c.Call(ctx, pathExtensionList, &out, q)
}

// InstallExtension installs an extension. ok is false if JDownloader refused.
func (c *Client) InstallExtension(ctx context.Context, id string) (ok bool, err error) {
	return ok, c.Call(ctx, pathExtensionInstall, &ok, id)
}

// SetExtensionEnabled enables or disables an extension. ok is false if
// JDownloader refused, for example for an extension that is not installed.
func (c *Client) SetExtensionEnabled(ctx context.Context, id string, enabled bool) (ok bool, err error) {
	return ok, c.Call(ctx, pathExtensionEnable, &ok, id, enabled)
}
