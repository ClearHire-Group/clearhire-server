package factory

import (
	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/user"
)

func InitUserFactory(repo user.Repository) *user.Handler {
	service := user.NewService(repo)
	return user.NewHandler(service)
}

func newUserRepository(db database.DB) user.Repository {
	return user.NewRepository(db)
}
