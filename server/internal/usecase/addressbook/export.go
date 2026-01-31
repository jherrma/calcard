package addressbook

import (
	"context"
	"fmt"
	"strings"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
)

type ExportUseCase struct {
	repo addressbook.Repository
}

func NewExportUseCase(repo addressbook.Repository) *ExportUseCase {
	return &ExportUseCase{repo: repo}
}

func (uc *ExportUseCase) Execute(ctx context.Context, id uint, userID uint) ([]byte, string, error) {
	ab, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if ab == nil || ab.UserID != userID {
		return nil, "", fmt.Errorf("address book not found")
	}

	// Fetch all contacts (AddressObjects) for this address book
	contacts, err := uc.repo.ListObjects(ctx, ab.ID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch contacts: %w", err)
	}

	var sb strings.Builder
	for _, contact := range contacts {
		sb.WriteString(contact.VCardData)
		sb.WriteString("\n")
	}

	filename := fmt.Sprintf("%s.vcf", ab.Name)
	return []byte(sb.String()), filename, nil
}
