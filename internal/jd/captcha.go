package jd

import "context"

const (
	pathCaptchaList  = "/captcha/list"
	pathCaptchaJob   = "/captcha/getCaptchaJob"
	pathCaptchaGet   = "/captcha/get"
	pathCaptchaSolve = "/captcha/solve"
	pathCaptchaSkip  = "/captcha/skip"
)

// CaptchaJob is a captcha JDownloader waits for.
type CaptchaJob struct {
	ID            int64  `json:"id"`
	Hoster        string `json:"hoster"`
	Type          string `json:"type"`
	Category      string `json:"captchaCategory"`
	ChallengeType string `json:"challengeType"`
	Explain       string `json:"explain"`
	Created       int64  `json:"created"`
	Timeout       int64  `json:"timeout"`
	Remaining     int    `json:"remaining"`
	Link          int64  `json:"link"`
}

// Captchas returns the waiting captchas.
func (c *Client) Captchas(ctx context.Context) (out []CaptchaJob, err error) {
	return out, c.Call(ctx, pathCaptchaList, &out)
}

// Captcha returns one captcha, or nil if JDownloader does not know the id.
func (c *Client) Captcha(ctx context.Context, id int64) (*CaptchaJob, error) {
	var j CaptchaJob
	if err := c.Call(ctx, pathCaptchaJob, &j, id); err != nil || j.ID == 0 {
		return nil, err
	}
	return &j, nil
}

// CaptchaImage returns the image of a captcha as a data URL. An unknown id is
// HTTP 404, see IsNotFound.
func (c *Client) CaptchaImage(ctx context.Context, id int64) (dataURL string, err error) {
	return dataURL, c.Call(ctx, pathCaptchaGet, &dataURL, id)
}

// CaptchaSolve sends the answer to a captcha. An unknown id is HTTP 404.
func (c *Client) CaptchaSolve(ctx context.Context, id int64, result string) error {
	return c.Call(ctx, pathCaptchaSolve, nil, id, result)
}

// CaptchaSkip skips a captcha. An unknown id is HTTP 404.
func (c *Client) CaptchaSkip(ctx context.Context, id int64, how SkipRequest) error {
	return c.Call(ctx, pathCaptchaSkip, nil, id, how)
}
