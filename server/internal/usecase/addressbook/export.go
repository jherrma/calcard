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
	contacts, _, err := uc.repo.ListObjects(ctx, ab.ID, -1, 0, "name", "asc")
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch contacts: %w", err)
	}

	var sb strings.Builder
	for _, contact := range contacts {
		sb.WriteString(contact.VCardData)
		sb.WriteString("\n")
	}

	filename := fmt.Sprintf("%s.vcf", sanitizeFilename(ab.Name))
	return []byte(sb.String()), filename, nil
}

// sanitizeFilename removes characters unsafe for filenames. Mirrors the helper
// in usecase/calendar/export.go (different package, so duplicated).
func sanitizeFilename(name string) string {
	replacer := map[rune]rune{
		'/': '-', '\\': '-', ':': '-', '*': '-', '?': '-',
		'"': '-', '<': '-', '>': '-', '|': '-',
	}
	result := []rune(name)
	for i, r := range result {
		if replacement, ok := replacer[r]; ok {
			result[i] = replacement
		}
	}
	return string(result)
}
