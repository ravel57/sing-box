package v2rayxhttp

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/sagernet/sing-box/option"
)

func normalize(options option.V2RayXHTTPOptions) (option.V2RayXHTTPOptions, *url.URL, error) {
	if options.Mode == "" {
		options.Mode = "auto"
	}
	switch options.Mode {
	case "auto", "packet-up", "stream-up", "stream-one":
	default:
		return options, nil, fmt.Errorf("xhttp: unknown mode: %s", options.Mode)
	}
	switch options.HTTPVersion {
	case "", "1.1", "2":
	default:
		return options, nil, fmt.Errorf("xhttp: unsupported HTTP version: %s", options.HTTPVersion)
	}
	if options.MaxPostSize == 0 {
		options.MaxPostSize = 1000000
	}
	if options.MaxBufferedPosts == 0 {
		options.MaxBufferedPosts = 30
	}
	if options.MaxPostSize < 1 || options.MaxPostSize > 16*1024*1024 || options.MaxBufferedPosts < 1 || options.MaxBufferedPosts > 1024 {
		return options, nil, fmt.Errorf("xhttp: invalid upload limits")
	}
	path := options.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u, err := url.ParseRequestURI(path)
	if err != nil {
		return options, nil, fmt.Errorf("xhttp: invalid path: %w", err)
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
		if u.RawPath != "" {
			u.RawPath += "/"
		}
	}
	return options, u, nil
}
