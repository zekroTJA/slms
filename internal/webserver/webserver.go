package webserver

import (
	"errors"
	"time"

	"github.com/go-gem/sessions"
	routing "github.com/qiangxue/fasthttp-routing"
	"github.com/valyala/fasthttp"
	"github.com/zekroTJA/slms/internal/auth"
	"github.com/zekroTJA/slms/internal/database"
)

// A WebServer handles the REST API
// connections.
type WebServer struct {
	db             database.Middleware
	auth           auth.Provider
	sessions       sessions.Store
	config         *Config
	server         *fasthttp.Server
	router         *routing.Router
	limitManager   *RateLimitManager
	redirectStatus int
}

// Config contains the configuration
// values for the WebServer.
type Config struct {
	Address           string     `json:"address"`
	RootRedirect      string     `json:"root_redirect"`
	OnlyHTTPSRootLink bool       `json:"only_https_rootlink"`
	PermanentRedirect bool       `json:"permanent_redirect"`
	APITokenHash      string     `json:"api_token_hash"`
	SessionStoreKey   string     `json:"session_store_key"`
	TLS               *ConfigTLS `json:"tls"`
}

// ConfigTLS contains the configuration
// values for TLS encryption for the
// WebServer.
type ConfigTLS struct {
	Use      bool   `json:"use"`
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
}

// NewWebServer creates a new instance
// of WebServer and registers all set
// request handlers.
func NewWebServer(conf *Config, db database.Middleware, authProvider auth.Provider) (*WebServer, error) {
	if len(conf.APITokenHash) < 8 {
		return nil, errors.New("api_token must have at least 8 characters")
	}

	router := routing.New()

	cookieStore := sessions.NewCookieStore([]byte(conf.SessionStoreKey))
	cookieStore.MaxAge(600)

	ws := &WebServer{
		auth:         authProvider,
		sessions:     cookieStore,
		db:           db,
		config:       conf,
		router:       router,
		limitManager: NewRateLimitManager(),
		server: &fasthttp.Server{
			Handler: sessions.ClearHandler(router.HandleRequest),
		},
	}

	if ws.config.PermanentRedirect {
		ws.redirectStatus = fasthttp.StatusPermanentRedirect
	} else {
		ws.redirectStatus = fasthttp.StatusTemporaryRedirect
	}

	ws.registerHandlers()

	return ws, nil
}

func (ws *WebServer) registerHandlers() {
	ws.router.Use(ws.handlerHeaderServer, ws.handlerFileServer)

	// GET /:SHORT
	ws.router.Get("/<short>", ws.handlerShort)

	// GROUP # /api
	api := ws.router.Group("/api")
	api.Use(ws.handlerAuth)

	// POST /api/login
	api.Post("/login",
		ws.limitManager.GetHandler(10*time.Second, 3),
		ws.handlerLogin)

	// GET /api/shortlinks/count
	api.Get("/shortlinks/count",
		ws.limitManager.GetHandler(1*time.Second, 10),
		ws.handlerGetShortLinkCount)
	// GET /api/shortlinks
	shortLinks := api.Get("/shortlinks",
		ws.limitManager.GetHandler(1*time.Second, 10),
		ws.handlerGetShortLinks)
	// POST /api/shortlinks
	shortLinks.Post(
		ws.limitManager.GetHandler(3*time.Second, 3),
		ws.handlerCreateShortLink)

	// GET /api/shortlinks/:ID
	shortLinksID := api.Get("/shortlinks/<id>",
		ws.limitManager.GetHandler(1*time.Second, 5),
		ws.handlerGetShortLink)
	// POST /api/shortlinks/:ID
	shortLinksID.Post(
		ws.limitManager.GetHandler(2*time.Second, 3),
		ws.handlerEditShortLink)
	// DELETE /api/shortlinks/:ID
	shortLinksID.Delete(
		ws.limitManager.GetHandler(2*time.Second, 5),
		ws.handlerDeleteShortLink)
}

// ListenAndServeBlocking starts listening for HTTP requests
// and serving responses to the specified address in the config.
// The server will run in TLS mode when set in the config.
// The startet event loop will block the current go routine.
func (ws *WebServer) ListenAndServeBlocking() error {
	if ws.config.TLS != nil && ws.config.TLS.Use {
		if ws.config.TLS.CertFile == "" || ws.config.TLS.KeyFile == "" {
			return errors.New("cert file and key file must be specified")
		}
		return ws.server.ListenAndServeTLS(
			ws.config.Address, ws.config.TLS.CertFile, ws.config.TLS.KeyFile)
	}

	return ws.server.ListenAndServe(ws.config.Address)
}
