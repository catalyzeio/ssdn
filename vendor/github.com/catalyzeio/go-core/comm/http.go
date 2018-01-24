package comm

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func HTTPClientFromURL(urlString string) (*http.Client, string, error) {
	u, err := url.Parse(urlString)
	if err != nil {
		return nil, "", err
	}
	// return client with custom transport for domain socket connections
	if u.Scheme == "unix" {
		path := u.Path
		transport := &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				return net.Dial("unix", path)
			},
		}
		fileName := path
		if ind := strings.LastIndex(fileName, "/"); ind >= 0 {
			fileName = fileName[ind+1:]
		}
		// a fake HTTP URL is necessary for the domain socket transport to function
		fakeURL := fmt.Sprintf("http://%s", fileName)
		return &http.Client{Transport: transport}, fakeURL, nil
	}
	// treat tcp and tcps schemes as HTTP
	if u.Scheme == "tcp" {
		u.Scheme = "http"
	} else if u.Scheme == "tcps" {
		u.Scheme = "https"
	}
	return http.DefaultClient, u.String(), nil
}

// Appends a raw path fragment to the request URI.
// Go's URL struct stores path data in decoded format; this method allows appending
// path elements without having them decoded by the URL parse method.
func AppendRawPath(r *http.Request, pathFragment string) {
	u := r.URL
	opaque := u.Opaque
	if len(opaque) > 0 {
		u.Opaque += pathFragment
	} else {
		u.Opaque = u.Path + pathFragment
	}
}
