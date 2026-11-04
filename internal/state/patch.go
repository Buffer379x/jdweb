package state

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
)

// Fields are the changed members of one link or package, with their JSON names.
type Fields map[string]any

// Patch is the payload of a patch event. It holds the latest value of what
// changed since the last Drain.
type Patch struct {
	List     List              `json:"list"`
	Links    map[string]Fields `json:"links"`
	Packages map[string]Fields `json:"packages"`
}

// pending collects the changes of one list until the next Drain. Later values
// overwrite earlier ones, which is the bundling.
type pending struct {
	links, packages map[int64]Fields
}

func newPending() pending {
	return pending{links: map[int64]Fields{}, packages: map[int64]Fields{}}
}

func (p *pending) empty() bool { return len(p.links) == 0 && len(p.packages) == 0 }

func (p *pending) reset() {
	clear(p.links)
	clear(p.packages)
}

func (p *pending) patch(list List) Patch {
	out := Patch{List: list, Links: make(map[string]Fields, len(p.links)), Packages: make(map[string]Fields, len(p.packages))}
	for k, v := range p.links {
		out.Links[strconv.FormatInt(k, 10)] = v
	}
	for k, v := range p.packages {
		out.Packages[strconv.FormatInt(k, 10)] = v
	}
	return out
}

// sink notes the changed fields of one entry. The map comes into being with the
// first real change, so a delta that changes nothing leaves no trace.
type sink struct {
	all map[int64]Fields
	id  int64
	cur Fields
}

func (s *sink) put(name string, v any) {
	if s.cur == nil {
		s.cur = s.all[s.id]
		if s.cur == nil {
			s.cur = Fields{}
			s.all[s.id] = s.cur
		}
	}
	s.cur[name] = v
}

// set writes v to *dst and notes the change.
func set[T comparable](s *sink, name string, dst *T, v T) {
	if *dst != v {
		*dst = v
		s.put(name, v)
	}
}

// setPtr is set for a value the event may not carry.
func setPtr[T comparable](s *sink, name string, dst *T, v *T) {
	if v != nil {
		set(s, name, dst, *v)
	}
}

// delta is the payload of a LINK_UPDATE or PACKAGE_UPDATE event: the uuid and
// the fields that changed. A nil pointer is a field the event does not carry.
type delta struct {
	UUID             *int64    `json:"uuid"`
	Name             *string   `json:"name"`
	Host             *string   `json:"host"`
	Hosts            *[]string `json:"hosts"`
	URL              *string   `json:"url"`
	ChildCount       *int      `json:"childCount"`
	BytesTotal       *int64    `json:"bytesTotal"`
	BytesLoaded      *int64    `json:"bytesLoaded"`
	Speed            *int64    `json:"speed"`
	ETA              *int64    `json:"eta"`
	Enabled          *bool     `json:"enabled"`
	Finished         *bool     `json:"finished"`
	Running          *bool     `json:"running"`
	Skipped          *bool     `json:"skipped"`
	SkipReason       *string   `json:"skipreason"` // lower case in revision 50639
	ExtractionStatus *string   `json:"extractionStatus"`
	Status           *string   `json:"status"`
	Priority         *string   `json:"priority"`
	Comment          *string   `json:"comment"`
	Password         *string   `json:"password"`
	DownloadPassword *string   `json:"downloadPassword"`
	AddedDate        *int64    `json:"addedDate"`
	FinishedDate     *int64    `json:"finishedDate"`
	SaveTo           *string   `json:"saveTo"`
	Availability     *string   `json:"availability"`
	OnlineCount      *int      `json:"onlineCount"`
	OfflineCount     *int      `json:"offlineCount"`
	UnknownCount     *int      `json:"unknownCount"`
	TempUnknownCount *int      `json:"tempUnknownCount"`
}

// decodeDelta reads an event payload. clean is false if a value had the wrong
// type: that field is left out and the others are kept. ok is false if the
// payload is no object at all.
func decodeDelta(data []byte) (d *delta, clean, ok bool) {
	d = new(delta)
	err := json.Unmarshal(data, d)
	if err == nil {
		return d, true, true
	}
	var wrongType *json.UnmarshalTypeError
	if !errors.As(err, &wrongType) {
		return nil, false, false
	}
	// encoding/json has set the pointer of a wrongly typed field to a zero value,
	// which looks like a real one. So go through the fields one by one. This path
	// is for answers that should not exist, it does not have to be fast.
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return nil, false, false
	}
	*d = delta{}
	for name, value := range fields {
		one, err := json.Marshal(map[string]json.RawMessage{name: value})
		if err != nil {
			continue
		}
		var probe delta
		if json.Unmarshal(one, &probe) == nil {
			_ = json.Unmarshal(one, d)
		}
	}
	return d, false, true
}

func orZero[T any](p **T) {
	if *p == nil {
		*p = new(T)
	}
}

// named lists the fields an event id can name, as in LINK_UPDATE.running.
// JDownloader leaves out false and zero values and sends null for empty text,
// so the named field falls back to its zero value when the event has no value.
var named = map[string]func(*delta){
	"name":             func(d *delta) { orZero(&d.Name) },
	"host":             func(d *delta) { orZero(&d.Host) },
	"hosts":            func(d *delta) { orZero(&d.Hosts) },
	"url":              func(d *delta) { orZero(&d.URL) },
	"childCount":       func(d *delta) { orZero(&d.ChildCount) },
	"bytesTotal":       func(d *delta) { orZero(&d.BytesTotal) },
	"bytesLoaded":      func(d *delta) { orZero(&d.BytesLoaded) },
	"speed":            func(d *delta) { orZero(&d.Speed) },
	"enabled":          func(d *delta) { orZero(&d.Enabled) },
	"finished":         func(d *delta) { orZero(&d.Finished) },
	"running":          func(d *delta) { orZero(&d.Running) },
	"skipped":          func(d *delta) { orZero(&d.Skipped) },
	"extractionStatus": func(d *delta) { orZero(&d.ExtractionStatus) },
	"status":           func(d *delta) { orZero(&d.Status) },
	"priority":         func(d *delta) { orZero(&d.Priority) },
	"comment":          func(d *delta) { orZero(&d.Comment) },
	"password":         func(d *delta) { orZero(&d.Password) },
	"addedDate":        func(d *delta) { orZero(&d.AddedDate) },
	"finishedDate":     func(d *delta) { orZero(&d.FinishedDate) },
	"saveTo":           func(d *delta) { orZero(&d.SaveTo) },
	"availability":     func(d *delta) { orZero(&d.Availability) },
	"onlineCount":      func(d *delta) { orZero(&d.OnlineCount) },
	"offlineCount":     func(d *delta) { orZero(&d.OfflineCount) },
	"unknownCount":     func(d *delta) { orZero(&d.UnknownCount) },
	"tempUnknownCount": func(d *delta) { orZero(&d.TempUnknownCount) },
	"eta": func(d *delta) { // no value is "nobody knows", not "zero seconds left"
		if d.ETA == nil {
			unknown := int64(ETAUnknown)
			d.ETA = &unknown
		}
	},
}

// notMirrored are fields JDownloader announces that the mirror has no use for.
// statusIconKey is a hash without stable meaning.
var notMirrored = map[string]bool{"statusIconKey": true, "advancedStatus": true, "jobUUID": true}

func (d *delta) password() *string {
	if d.DownloadPassword != nil {
		return d.DownloadPassword
	}
	return d.Password
}

func (d *delta) applyDlLink(l *DlLink, s *sink) {
	setPtr(s, "name", &l.Name, d.Name)
	setPtr(s, "host", &l.Host, d.Host)
	setPtr(s, "url", &l.URL, d.URL)
	setPtr(s, "bytesTotal", &l.BytesTotal, d.BytesTotal)
	setPtr(s, "bytesLoaded", &l.BytesLoaded, d.BytesLoaded)
	setPtr(s, "enabled", &l.Enabled, d.Enabled)
	setPtr(s, "finished", &l.Finished, d.Finished)
	setPtr(s, "running", &l.Running, d.Running)
	setPtr(s, "skipped", &l.Skipped, d.Skipped)
	if d.Skipped != nil && !*d.Skipped {
		set(s, "skipReason", &l.SkipReason, "")
	}
	setPtr(s, "skipReason", &l.SkipReason, d.SkipReason)
	setPtr(s, "extractionStatus", &l.ExtractionStatus, d.ExtractionStatus)
	setPtr(s, "status", &l.Status, d.Status)
	setPtr(s, "priority", &l.Priority, d.Priority)
	setPtr(s, "comment", &l.Comment, d.Comment)
	setPtr(s, "downloadPassword", &l.DownloadPassword, d.password())
	setPtr(s, "addedDate", &l.AddedDate, d.AddedDate)
	setPtr(s, "finishedDate", &l.FinishedDate, d.FinishedDate)
	if d.Speed != nil {
		set(s, "speed", &l.Speed, max(*d.Speed, 0))
	}
	if d.ETA != nil {
		set(s, "eta", &l.ETA, NewETA(*d.ETA))
	}
	settleDlLink(l, s)
}

// settleDlLink restores what must hold for every link: a link that does not
// run has no speed and no eta, also when a progress event comes in late, and
// state follows the other fields.
func settleDlLink(l *DlLink, s *sink) {
	// A link that runs again has no final state any more. Events cannot say so, the next reload of the list does.
	if l.Running && l.FinalState != "" {
		set(s, "finalState", &l.FinalState, "")
	}
	if !l.Running {
		set(s, "speed", &l.Speed, 0)
		set(s, "eta", &l.ETA, ETAUnknown)
	}
	set(s, "state", &l.State, deriveLinkState(*l))
}

func (d *delta) applyDlPackage(p *DlPackage, s *sink) {
	setPtr(s, "name", &p.Name, d.Name)
	d.applyHosts(&p.Hosts, s)
	setPtr(s, "childCount", &p.ChildCount, d.ChildCount)
	setPtr(s, "bytesTotal", &p.BytesTotal, d.BytesTotal)
	setPtr(s, "bytesLoaded", &p.BytesLoaded, d.BytesLoaded)
	setPtr(s, "enabled", &p.Enabled, d.Enabled)
	setPtr(s, "finished", &p.Finished, d.Finished)
	setPtr(s, "running", &p.Running, d.Running)
	setPtr(s, "saveTo", &p.SaveTo, d.SaveTo)
	setPtr(s, "status", &p.Status, d.Status)
	setPtr(s, "priority", &p.Priority, d.Priority)
	setPtr(s, "comment", &p.Comment, d.Comment)
	setPtr(s, "downloadPassword", &p.DownloadPassword, d.password())
	if d.Speed != nil {
		set(s, "speed", &p.Speed, max(*d.Speed, 0))
	}
	if d.ETA != nil {
		set(s, "eta", &p.ETA, NewETA(*d.ETA))
	}
	settleDlPackage(p, s)
}

func settleDlPackage(p *DlPackage, s *sink) {
	if !p.Running {
		set(s, "speed", &p.Speed, 0)
		set(s, "eta", &p.ETA, ETAUnknown)
	}
}

func (d *delta) applyLgLink(l *LgLink, s *sink) {
	setPtr(s, "name", &l.Name, d.Name)
	setPtr(s, "host", &l.Host, d.Host)
	setPtr(s, "url", &l.URL, d.URL)
	setPtr(s, "bytesTotal", &l.BytesTotal, d.BytesTotal)
	setPtr(s, "availability", &l.Availability, d.Availability)
	setPtr(s, "enabled", &l.Enabled, d.Enabled)
	setPtr(s, "priority", &l.Priority, d.Priority)
	setPtr(s, "comment", &l.Comment, d.Comment)
	setPtr(s, "downloadPassword", &l.DownloadPassword, d.password())
}

func (d *delta) applyLgPackage(p *LgPackage, s *sink) {
	setPtr(s, "name", &p.Name, d.Name)
	d.applyHosts(&p.Hosts, s)
	setPtr(s, "childCount", &p.ChildCount, d.ChildCount)
	setPtr(s, "bytesTotal", &p.BytesTotal, d.BytesTotal)
	setPtr(s, "saveTo", &p.SaveTo, d.SaveTo)
	setPtr(s, "onlineCount", &p.OnlineCount, d.OnlineCount)
	setPtr(s, "offlineCount", &p.OfflineCount, d.OfflineCount)
	setPtr(s, "unknownCount", &p.UnknownCount, d.UnknownCount)
	setPtr(s, "tempUnknownCount", &p.TempUnknownCount, d.TempUnknownCount)
	setPtr(s, "enabled", &p.Enabled, d.Enabled)
	setPtr(s, "priority", &p.Priority, d.Priority)
	setPtr(s, "comment", &p.Comment, d.Comment)
	setPtr(s, "downloadPassword", &p.DownloadPassword, d.password())
}

func (d *delta) applyHosts(dst *[]string, s *sink) {
	if d.Hosts != nil && !reflect.DeepEqual(*dst, orEmpty(*d.Hosts)) {
		*dst = orEmpty(*d.Hosts)
		s.put("hosts", *dst)
	}
}

// fieldInfo ties a struct field to its JSON name.
type fieldInfo struct {
	name  string
	index int
}

func fieldsOf[T any]() []fieldInfo {
	t := reflect.TypeFor[T]()
	out := make([]fieldInfo, 0, t.NumField())
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		out = append(out, fieldInfo{name, i})
	}
	return out
}

// The field tables of the structs a reload compares.
var (
	dlPackageFields = fieldsOf[DlPackage]()
	dlLinkFields    = fieldsOf[DlLink]()
	lgPackageFields = fieldsOf[LgPackage]()
	lgLinkFields    = fieldsOf[LgLink]()
)

// diff notes the fields that differ between the old and the new entry. A reload
// finds what no event announced; the browser gets it as an ordinary patch.
func diff[T any](fields []fieldInfo, old, cur *T, s *sink) {
	if reflect.DeepEqual(old, cur) {
		return
	}
	a, b := reflect.ValueOf(old).Elem(), reflect.ValueOf(cur).Elem()
	for _, f := range fields {
		av, bv := a.Field(f.index).Interface(), b.Field(f.index).Interface()
		if !reflect.DeepEqual(av, bv) {
			s.put(f.name, bv)
		}
	}
}
