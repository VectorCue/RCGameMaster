package web

import (
	"RCJVGameMaster/src/config"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/static"
	"gorm.io/gorm"
)

type API struct {
	CFG  *config.Config
	DB   *gorm.DB
	Auth *Auth
}

func newApp(cfg *config.Config, db *gorm.DB) (*fiber.App, error) {
	auth, err := NewAuth(cfg.Auth)
	if err != nil {
		return nil, err
	}
	a := API{
		CFG:  cfg,
		DB:   db,
		Auth: auth,
	}

	app := fiber.New(fiber.Config{ServerHeader: "rcjvgamemaster:fiber", AppName: "RCJVGameMaster", Immutable: true})
	app.Use(requestLogging)
	app.Use(func(c fiber.Ctx) error {
		c.Set("X-Server-Time", time.Now().UTC().Format(time.RFC3339Nano))
		c.Set("Referrer-Policy", "no-referrer")
		c.Set("X-Content-Type-Options", "nosniff")
		if len(c.Path()) >= 5 && (c.Path()[:5] == "/auth" || c.Path()[:5] == "/api/") {
			c.Set("Cache-Control", "no-store")
		}
		return c.Next()
	})
	app.Get("/healthcheck", getHealthCheck)
	app.Get("/auth/login", auth.Login)
	app.Get("/auth/callback", auth.Callback)
	a.registerPublicRoutes(app)

	v1 := app.Group("/api/v1", auth.CSRF)
	v1.Get("/auth/login", auth.Login)
	v1.Post("/auth/logout", auth.Logout)
	v1.Use(a.Auth.RequireAuth)
	v1.Get("/auth/me", auth.Me)
	a.registerDomainRoutes(v1)
	a.registerMatchRoutes(v1)
	// Hash routing lets the built Vue application share the API's origin.
	if _, err := os.Stat("frontend/dist/index.html"); err == nil {
		app.Use("/", static.New("frontend/dist"))
	}
	app.Use(func(c fiber.Ctx) error {
		return c.Status(404).JSON(fiber.Map{"error": "Route not found."})
	})

	return app, nil
}

func InitWeb(cfg *config.Config, db *gorm.DB) {
	app, err := newApp(cfg, db)
	if err != nil {
		log.Fatalf("Invalid application configuration: %v", err)
	}
	if cfg.Auth.DisableFusionAuth {
		log.Print("Local development mode: FusionAuth disabled; requests run as local admin. Listening on loopback only.")
	}
	slog.Info("server.starting", "listen_address", listenAddress(cfg),
		"frontend_url", logURL(cfg.Auth.FrontendURL), "callback_url", logURL(cfg.Auth.OAuthRedirectURI),
		"fusionauth_url", logURL(cfg.Auth.FusionAuthURL), "fusionauth_disabled", cfg.Auth.DisableFusionAuth)
	log.Fatal(app.Listen(listenAddress(cfg)))
}

func listenAddress(cfg *config.Config) string {
	if cfg.Auth.DisableFusionAuth {
		return "127.0.0.1:2006"
	}
	return "0.0.0.0:2006"
}
