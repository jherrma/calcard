package importexport

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
)

// ContactImportUseCase handles contact import from vCard format
type ContactImportUseCase struct {
	addressBookRepo addressbook.Repository
}

// NewContactImportUseCase creates a new contact import use case
func NewContactImportUseCase(addressBookRepo addressbook.Repository) *ContactImportUseCase {
	return &ContactImportUseCase{addressBookRepo: addressBookRepo}
}

// Execute imports contacts from vCard data
func (uc *ContactImportUseCase) Execute(ctx context.Context, userID uint, addressBookID uint, data []byte, opts ImportOptions) (*ImportResult, error) {
	// Get address book and verify ownership
	ab, err := uc.addressBookRepo.GetByID(ctx, addressBookID)
	if err != nil {
		return nil, fmt.Errorf("address book not found")
	}
	if ab == nil || ab.UserID != userID {
		return nil, fmt.Errorf("access denied")
	}

	// Default options
	if opts.DuplicateHandling == "" {
		opts.DuplicateHandling = "skip"
	}

	// Split vCard data into individual cards
	vcards := splitVCards(string(data))

	result := &ImportResult{
		Total: len(vcards),
	}

	for i, vcardData := range vcards {
		vcardData = strings.TrimSpace(vcardData)
		if vcardData == "" {
			continue
		}

		// Ensure CRLF line endings
		vcardData = normalizeLineEndings(vcardData)

		// Extract UID
		uid := extractVCardUID(vcardData)
		if uid == "" {
			// Generate a UID if missing
			uid = uuid.New().String()
			vcardData = injectVCardUID(vcardData, uid)
		}

		// Extract FN for error reporting
		fn := extractVCardFN(vcardData)

		// Check for existing contact by UID
		existing, _ := uc.addressBookRepo.GetObjectByUUID(ctx, uid)

		if existing != nil && existing.AddressBookID == addressBookID {
			switch opts.DuplicateHandling {
			case "skip":
				result.Skipped++
				continue
			case "replace":
				// Delete existing object
				if err := uc.addressBookRepo.DeleteObjectByUUID(ctx, uid); err != nil {
					result.Failed++
					result.Errors = append(result.Errors, ImportError{
						Index:   i,
						UID:     uid,
						Summary: fn,
						Error:   fmt.Sprintf("failed to delete existing: %v", err),
					})
					continue
				}
			case "duplicate":
				// Generate new UID
				uid = uuid.New().String()
				vcardData = replaceVCardUID(vcardData, uid)
			}
		}

		// Create address object
		obj := &addressbook.AddressObject{
			AddressBookID: addressBookID,
			UID:           uid,
			Path:          fmt.Sprintf("%s.vcf", uid),
			ETag:          fmt.Sprintf("\"%d\"", time.Now().UnixNano()),
			VCardData:     vcardData,
		}

		if err := uc.addressBookRepo.CreateObject(ctx, obj); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, ImportError{
				Index:   i,
				UID:     uid,
				Summary: fn,
				Error:   fmt.Sprintf("failed to create: %v", err),
			})
			continue
		}

		result.Imported++
	}

	// Update address book CTag
	ab.CTag = fmt.Sprintf("ctag-%d", time.Now().UnixNano())
	_ = uc.addressBookRepo.Update(ctx, ab)

	return result, nil
}

// splitVCards splits a vCard file containing multiple cards into individual cards
func splitVCards(data string) []string {
	var cards []string
	data = normalizeLineEndings(data)

	// Split by BEGIN:VCARD
	parts := strings.Split(data, "BEGIN:VCARD")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// Re-add the BEGIN:VCARD
		cards = append(cards, "BEGIN:VCARD\r\n"+part)
	}

	return cards
}

// normalizeLineEndings converts all line endings to CRLF
func normalizeLineEndings(data string) string {
	// First normalize to LF, then convert to CRLF
	data = strings.ReplaceAll(data, "\r\n", "\n")
	data = strings.ReplaceAll(data, "\r", "\n")
	data = strings.ReplaceAll(data, "\n", "\r\n")
	return data
}

// extractVCardUID extracts the UID from vCard data
func extractVCardUID(data string) string {
	for _, line := range strings.Split(data, "\r\n") {
		if strings.HasPrefix(strings.ToUpper(line), "UID:") {
			return strings.TrimPrefix(line, "UID:")
		}
	}
	return ""
}

// extractVCardFN extracts the FN (formatted name) from vCard data
func extractVCardFN(data string) string {
	for _, line := range strings.Split(data, "\r\n") {
		if strings.HasPrefix(strings.ToUpper(line), "FN:") {
			return strings.TrimPrefix(line, "FN:")
		}
	}
	return ""
}

// injectVCardUID adds a UID to vCard data
func injectVCardUID(data, uid string) string {
	// Insert UID after VERSION line
	lines := strings.Split(data, "\r\n")
	var result []string
	uidAdded := false
	for _, line := range lines {
		result = append(result, line)
		if !uidAdded && strings.HasPrefix(strings.ToUpper(line), "VERSION:") {
			result = append(result, "UID:"+uid)
			uidAdded = true
		}
	}
	return strings.Join(result, "\r\n")
}

// replaceVCardUID replaces the UID in vCard data
func replaceVCardUID(data, newUID string) string {
	lines := strings.Split(data, "\r\n")
	var result []string
	for _, line := range lines {
		if strings.HasPrefix(strings.ToUpper(line), "UID:") {
			result = append(result, "UID:"+newUID)
		} else {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\r\n")
}
