package jd

import "context"

const (
	pathDialogList = "/dialogs/list"
	pathDialogGet  = "/dialogs/get"
	pathDialogType = "/dialogs/getTypeInfo"
	pathDialogAns  = "/dialogs/answer"
)

// Dialog is a question JDownloader waits for. Type is a Java interface name
// such as "jd.controlling.downloadcontroller.IfFileExistsDialogInterface".
type Dialog struct {
	Type       string            `json:"type"`
	Properties map[string]string `json:"properties"`
}

// DialogTypeInfo describes the properties of a dialog type (Out) and what an
// answer may contain (In), with Java type names as values.
type DialogTypeInfo struct {
	In  map[string]string `json:"in"`
	Out map[string]string `json:"out"`
}

// DialogIDs returns the ids of the open dialogs. JDownloader asks them one
// after the other. Recorded with two open dialogs: answering the first id of
// the list fails with HTTP 400 and the error type "Answer Dialog <id> first".
// An unknown id fails with HTTP 400 and the type "Invalid ID: <id>". Both
// texts are in the Type field of *Error.
func (c *Client) DialogIDs(ctx context.Context) (out []int64, err error) {
	return out, c.Call(ctx, pathDialogList, &out)
}

// Dialog returns a dialog with its properties but without the icon.
func (c *Client) Dialog(ctx context.Context, id int64) (d Dialog, err error) {
	return d, c.Call(ctx, pathDialogGet, &d, id, false, true)
}

// DialogType returns the description of a dialog type.
func (c *Client) DialogType(ctx context.Context, dialogType string) (t DialogTypeInfo, err error) {
	return t, c.Call(ctx, pathDialogType, &t, dialogType)
}

// DialogAnswer answers a dialog. The keys follow DialogTypeInfo.In, for
// example {"closereason":"OK"}.
func (c *Client) DialogAnswer(ctx context.Context, id int64, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	return c.Call(ctx, pathDialogAns, nil, id, data)
}
