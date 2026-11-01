package jd

import "context"

const (
	pathHosterIcon = "/contentV2/getFavIcon"
	pathFileIcon   = "/contentV2/getFileIcon"
)

// HosterIcon returns the icon of a hoster (a host name such as
// "1fichier.com") as image bytes and their Content-Type, "image/png" in
// revision 50639 (16x16). JDownloader answers an unknown host with a generic
// icon, not an error. This is a binary answer, not the usual JSON.
func (c *Client) HosterIcon(ctx context.Context, hostname string) (data []byte, contentType string, err error) {
	return c.callImage(ctx, pathHosterIcon, hostname)
}

// FileIcon returns the icon for a file name (chosen by its extension) as image
// bytes and their Content-Type, see HosterIcon.
func (c *Client) FileIcon(ctx context.Context, filename string) (data []byte, contentType string, err error) {
	return c.callImage(ctx, pathFileIcon, filename)
}
