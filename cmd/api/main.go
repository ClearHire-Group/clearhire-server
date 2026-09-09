// clearhire-server — API da plataforma. Este arquivo só faz bootstrap:
// carrega config, abre conexão com o banco, monta a factory e sobe o Fiber.
// Nenhuma lógica de negócio mora aqui.
package main

import (
	"context"
	"log"

	"github.com/ClearHire-Group/clearhire-server/internal/config"
	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/internal/factory"
	"github.com/ClearHire-Group/clearhire-server/internal/server"
	"github.com/ClearHire-Group/clearhire-server/pkg/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("falha ao carregar configuração: %v", err)
	}

	logger.Init(cfg.Env)

	ctx := context.Background()

	db, err := database.NewPostgresPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("falha ao conectar no banco: %v", err)
	}
	defer db.Close()

	f := factory.New(db, cfg)

	app := server.New(cfg)
	server.RegisterRoutes(app, f, cfg)

	if err := app.Listen(":" + cfg.Port); err != nil {
		log.Fatalf("falha ao iniciar o servidor: %v", err)
	}
}
