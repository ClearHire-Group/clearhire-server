package factory

import (
	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/user"
	"github.com/ClearHire-Group/clearhire-server/pkg/mailer"
)

func InitUserFactory(repo user.Repository, sender mailer.Sender, frontendBaseURL string, isDevelopment bool) *user.Handler {
	service := user.NewService(repo, sender, frontendBaseURL)
	return user.NewHandler(service, isDevelopment)
}

func newUserRepository(db database.DB) user.Repository {
	return user.NewRepository(db)
}
