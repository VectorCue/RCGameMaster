package web

import (
	_ "embed"
	"github.com/gofiber/fiber/v3"
)

//go:embed docs/rcgamemaster_restapi_v1_openapi.yaml
var restOpenAPISpecification []byte

func getRESTOpenAPI(c fiber.Ctx) error {
	c.Set("Content-Type", "application/yaml")
	return c.Send(restOpenAPISpecification)
}
