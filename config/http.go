package config

import (
	"github.com/goravel/framework/contracts/route"
	ginfacades "github.com/goravel/gin/facades"
	"goravel/app/facades"
)

func init() {
	config := facades.Config()
	config.Add("http", map[string]any{
		"default": "gin",
		// HTTP Drivers
		"drivers": map[string]any{
			"gin": map[string]any{
				"body_limit":   4096,
				"header_limit": 4096,
				"route": func() (route.Route, error) {
					return ginfacades.Route("gin"), nil
				},
			},
		},
		// HTTP URL
		"url": config.Env("APP_URL", "http://localhost"),
		// HTTP Host
		"host": config.Env("APP_HOST", "127.0.0.1"),
		// HTTP Port
		"port": config.Env("APP_PORT", "3000"),
		// HTTP Timeout. This is a single GLOBAL deadline the gin driver
		// applies to every request (see github.com/goravel/gin route.go,
		// "Timeout" in its global middleware list, wired in via
		// engine.Use() - there's no per-route override for it in this
		// framework version). It must be at least as large as the longest
		// any single request is allowed to legitimately take.
		//
		// That's now GET /images/{id}/wait (a long-poll - see
		// ImageController.Wait), which deliberately blocks for up to
		// image.status_wait_seconds (see config/image.go) before
		// responding - this must stay comfortably larger than that, or the
		// framework force-cancels the request out from under the handler
		// before it gets to respond on its own. IMPORTANT: this middleware
		// also fully buffers every response and only releases it to the
		// client once the handler returns OR this deadline fires (in which
		// case the buffered response is discarded, not delivered) - so
		// this value being large is not "extra safety margin" the way it
		// would be for a normal request timeout, it's load-bearing for
		// Wait to ever respond at all. Every other endpoint here finishes
		// in milliseconds, so this value being large doesn't weaken
		// anything for them in practice.
		"request_timeout": config.Env("HTTP_REQUEST_TIMEOUT", 35),
		// HTTPS Configuration
		"tls": map[string]any{
			// HTTPS Host
			"host": config.Env("APP_HOST", "127.0.0.1"),
			// HTTPS Port
			"port": config.Env("APP_PORT", "3000"),
			// SSL Certificate, you can put the certificate in /public folder
			"ssl": map[string]any{
				// ca.pem
				"cert": "",
				// ca.key
				"key": "",
			},
		},
		// Default Client Name
		//
		// This determines which client is used when you call facades.Http() or
		// facades.Http().Client() without passing a specific name.
		"default_client": config.Env("HTTP_CLIENT_DEFAULT", "default"),
		// Client Configurations
		//
		// Here you may define multiple independent client configurations.
		// For example, you might have a "github" client with a specific base URL
		// and a "stripe" client with a longer timeout.
		"clients": map[string]any{
			"default": map[string]any{
				// The base URL for the client. All requests made using this client
				// will be relative to this URL.
				"base_url": config.Env("HTTP_CLIENT_BASE_URL", ""),
				// The maximum amount of time a request can take, including connection
				// establishment, redirects, and reading the response body.
				"timeout": config.Env("HTTP_CLIENT_TIMEOUT", "30s"),
				// The maximum number of idle (keep-alive) connections to keep across
				// ALL hosts. Increasing this helps reuse TCP connections.
				"max_idle_conns": config.Env("HTTP_CLIENT_MAX_IDLE_CONNS", 100),
				// The maximum number of idle (keep-alive) connections to keep PER host.
				// By default, Go sets this to 2, which is often a bottleneck.
				// Increase this value for high-throughput applications.
				"max_idle_conns_per_host": config.Env("HTTP_CLIENT_MAX_IDLE_CONNS_PER_HOST", 2),
				// The maximum total number of connections (active + idle) allowed per host.
				// A value of 0 means no limit.
				"max_conns_per_host": config.Env("HTTP_CLIENT_MAX_CONN_PER_HOST", 0),
				// The maximum amount of time an idle (keep-alive) connection will remain
				// in the pool before closing itself.
				"idle_conn_timeout": config.Env("HTTP_CLIENT_IDLE_CONN_TIMEOUT", "90s"),
			},
		},
	})
}
