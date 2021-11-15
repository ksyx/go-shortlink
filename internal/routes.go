package internal

import (
	"encoding/binary"
	"errors"
	"io"
	"io/ioutil"
	"strings"
	"time"
)

// Route is the value part of a shortcut.
type Route struct {
	URL  string    `json:"url"`
	Time time.Time `json:"time"`
	User string    `json:"user"`
}

// RouteIterator allows iteration of the named routes in the store.
type RouteIterator interface {
	Valid() bool
	Next() bool
	Seek(string) bool
	Error() error
	Name() string
	Route() *Route
	Release()
}

var ErrRouteNotFound = errors.New("route not found")

// Serialize this Route into the given Writer.
func (o *Route) Write(w io.Writer) error {
	if err := binary.Write(w, binary.LittleEndian, o.Time.UnixNano()); err != nil {
		return err
	}

	if _, err := w.Write([]byte(o.User + "$")); err != nil {
		return err
	}

	if _, err := w.Write([]byte(o.URL)); err != nil {
		return err
	}

	return nil
}

// Deserialize this Route from the given Reader.
func (o *Route) Read(r io.Reader) error {
	var t int64
	if err := binary.Read(r, binary.LittleEndian, &t); err != nil {
		return err
	}

	b, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	str := string(b)
	ind := strings.Index(str, "$")
	if ind != -1 {
		o.User = str[:ind]
		str = str[ind+1:]
	}
	o.URL = str
	o.Time = time.Unix(0, t)
	return nil
}
