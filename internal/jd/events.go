package jd

import "context"

const (
	pathEventsSubscribe = "/events/subscribe"
	pathEventsSet       = "/events/setsubscription"
	pathEventsStatus    = "/events/getsubscriptionstatus"
	pathEventsListen    = "/events/listen"
	pathEventsUnsub     = "/events/unsubscribe"
	pathEventsPublisher = "/events/listpublisher"
	pathEventsInterval  = "/downloadevents/setStatusEventInterval"
)

// Subscription is an event subscription of JDownloader.
type Subscription struct {
	ID             int64    `json:"subscriptionid"`
	Subscriptions  []string `json:"subscriptions"`
	Exclusions     []string `json:"exclusions"`
	MaxPollTimeout int64    `json:"maxPolltimeout"` // milliseconds, how long one listen may block
	MaxKeepalive   int64    `json:"maxKeepalive"`   // milliseconds until JDownloader drops an idle subscription
}

// SubscriptionStatus is the answer of getsubscriptionstatus.
type SubscriptionStatus struct {
	ID        int64 `json:"subscriptionid"`
	QueueSize int   `json:"queueSize"`
	// Subscribed is false even for a live subscription in revision 50639. Do
	// not use it, use Exists.
	Subscribed bool `json:"subscribed"`
}

// Exists reports whether JDownloader still knows the subscription id. A
// restarted JDownloader answers with subscriptionid -1.
func (s SubscriptionStatus) Exists(id int64) bool { return s.ID == id && id > 0 }

// PublisherInfo is a publisher and the event ids it can send.
type PublisherInfo struct {
	Publisher string   `json:"publisher"`
	EventIDs  []string `json:"eventids"`
}

// Subscribe creates a subscription for the given publishers, see Publishers.
func (c *Client) Subscribe(ctx context.Context, publishers []string) (s Subscription, err error) {
	return s, c.Call(ctx, pathEventsSubscribe, &s, publishers, []string{})
}

// SetSubscription replaces the publishers of a subscription.
func (c *Client) SetSubscription(ctx context.Context, id int64, publishers []string) (s Subscription, err error) {
	return s, c.Call(ctx, pathEventsSet, &s, id, publishers, []string{})
}

// SubscriptionStatus asks whether JDownloader still knows a subscription.
func (c *Client) SubscriptionStatus(ctx context.Context, id int64) (s SubscriptionStatus, err error) {
	return s, c.Call(ctx, pathEventsStatus, &s, id)
}

// Listen waits for events of a subscription. It returns an empty list after
// the poll timeout (25 s). When JDownloader does not know the id any more
// (it restarted) the error has HTTP status 404, see IsNotFound.
func (c *Client) Listen(ctx context.Context, id int64) (out []Event, err error) {
	return out, c.Call(ctx, pathEventsListen, &out, id)
}

// Unsubscribe removes a subscription.
func (c *Client) Unsubscribe(ctx context.Context, id int64) error {
	return c.Call(ctx, pathEventsUnsub, nil, id)
}

// ListPublishers returns the publishers JDownloader offers.
func (c *Client) ListPublishers(ctx context.Context) (out []PublisherInfo, err error) {
	return out, c.Call(ctx, pathEventsPublisher, &out)
}

// SetStatusEventInterval sets how often a subscription gets progress events,
// in milliseconds.
func (c *Client) SetStatusEventInterval(ctx context.Context, id int64, ms int64) error {
	return c.Call(ctx, pathEventsInterval, nil, id, ms)
}
