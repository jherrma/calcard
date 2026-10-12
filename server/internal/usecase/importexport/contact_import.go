package importexport

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/emersion/go-vcard"
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
	// Verify WRITE access to the target book. Permission-based rather than
	// owner-only (#53) so importing a .vcf into a read-write shared book works —
	// otherwise a sharee could add contacts one at a time but not in bulk, which
	// is the same capability with a different button.
	perm, err := uc.addressBookRepo.GetUserPermission(ctx, addressBookID, userID)
	if err != nil {
		return nil, fmt.Errorf("address book not found")
	}
	if !perm.CanWrite() {
		return nil, fmt.Errorf("access denied")
	}

	ab, err := uc.addressBookRepo.GetByID(ctx, addressBookID)
	if err != nil || ab == nil {
		return nil, fmt.Errorf("address book not found")
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

		// A card a CardDAV client cannot read must not be stored: it is
		// served as-is later, and a missing VERSION even turns its DAV GET
		// into a 500.
		card, err := parseImportCard(vcardData)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, ImportError{
				Index:   i,
				UID:     extractVCardUID(vcardData),
				Summary: extractVCardFN(vcardData),
				Error:   err.Error(),
			})
			continue
		}

		// Extract UID
		uid := card.Value(vcard.FieldUID)
		if uid == "" {
			// Generate a UID if missing
			uid = uuid.New().String()
			card.SetValue(vcard.FieldUID, uid)
		}

		// FN for error reporting (parseImportCard made sure there is one)
		fn := card.PreferredValue(vcard.FieldFormattedName)

		// Check for an existing contact with the same vCard UID in this book.
		// Look up by vCard UID (GetObjectByUID), not the internal DB UUID —
		// they are different identifiers, so the old GetObjectByUUID(uid) never
		// matched and every re-import duplicated.
		existing, _ := uc.addressBookRepo.GetObjectByUID(ctx, addressBookID, uid)
		// A replaced contact keeps its path, so CardDAV clients see an update.
		preferredPath := ""

		if existing != nil {
			switch opts.DuplicateHandling {
			case "skip":
				result.Skipped++
				continue
			case "replace":
				// Delete by the existing object's internal UUID, not the vCard UID.
				if err := uc.addressBookRepo.DeleteObjectByUUID(ctx, existing.UUID); err != nil {
					result.Failed++
					result.Errors = append(result.Errors, ImportError{
						Index:   i,
						UID:     uid,
						Summary: fn,
						Error:   fmt.Sprintf("failed to delete existing: %v", err),
					})
					continue
				}
				preferredPath = existing.Path
			case "duplicate":
				// Generate new UID
				uid = uuid.New().String()
				card.SetValue(vcard.FieldUID, uid)
			}
		}

		// Store the card the way the REST and CardDAV write paths do: as the
		// go-vcard encoding of the parsed card.
		vcardData, err = encodeImportCard(card)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, ImportError{
				Index:   i,
				UID:     uid,
				Summary: fn,
				Error:   fmt.Sprintf("invalid vCard: %v", err),
			})
			continue
		}

		// Create address object. The internal DB UUID must be unique and
		// non-empty (the column has a unique index and NOT NULL) — otherwise
		// the second row in a multi-contact import collides on uuid="".
		obj := &addressbook.AddressObject{
			UUID:          uuid.New().String(),
			AddressBookID: addressBookID,
			UID:           uid,
			Path:          importObjectPath(uid, ".vcf", preferredPath, uc.pathTaken(ctx, addressBookID)),
			ETag:          addressbook.NewETag(),
			VCardData:     vcardData,
			VCardVersion:  card.Value(vcard.FieldVersion),
			ContentLength: len(vcardData),
		}
		addressbook.ExtractDenormFieldsFromCard(card, obj)

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

	// Each CreateObject already advanced the address book's sync_token/ctag (and
	// wrote a change-log row) inside its transaction. Do NOT re-Save the stale
	// `ab` struct here — that would clobber those tokens and desync the change
	// log from the address book row.

	return result, nil
}

// pathTaken reports whether a live contact in the book already uses a path.
// A lookup error counts as free, which is no worse than before the check.
func (uc *ContactImportUseCase) pathTaken(ctx context.Context, addressBookID uint) func(string) bool {
	return func(path string) bool {
		obj, err := uc.addressBookRepo.GetObjectByPath(ctx, addressBookID, path)
		return err == nil && obj != nil
	}
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
			return line[len("UID:"):]
		}
	}
	return ""
}

// extractVCardFN extracts the FN (formatted name) from vCard data
func extractVCardFN(data string) string {
	for _, line := range strings.Split(data, "\r\n") {
		if strings.HasPrefix(strings.ToUpper(line), "FN:") {
			return line[len("FN:"):]
		}
	}
	return ""
}

// importVCardVersions are the vCard versions an import accepts: exactly the
// supported-address-data every CardDAV address book advertises (RFC 6352
// §6.2.2). A 2.1 card (QUOTED-PRINTABLE, bare TYPE values) would be served to
// clients that were promised 3.0 or 4.0.
var importVCardVersions = []string{"3.0", "4.0"}

// parseImportCard decodes one card of an import and checks what a CardDAV
// client needs to read it back: a VERSION it was promised (RFC 6350 §6.7.9)
// and an FN (RFC 6350 §6.2.1). A missing FN is derived from N, ORG, … rather
// than refused; only a card with nothing to derive it from fails. The error
// text is the reason reported in the import result.
func parseImportCard(data string) (vcard.Card, error) {
	card, err := vcard.NewDecoder(strings.NewReader(data)).Decode()
	if err != nil {
		return nil, fmt.Errorf("invalid vCard: %v", err)
	}
	version := strings.TrimSpace(card.Value(vcard.FieldVersion))
	if version == "" {
		return nil, fmt.Errorf("invalid vCard: missing VERSION property")
	}
	if !slices.Contains(importVCardVersions, version) {
		return nil, fmt.Errorf("unsupported vCard version %q (3.0 and 4.0 are supported)", version)
	}
	card.SetValue(vcard.FieldVersion, version)
	if !addressbook.EnsureFormattedName(card) {
		return nil, fmt.Errorf("invalid vCard: no FN and no N, ORG, NICKNAME, EMAIL or TEL to derive one from")
	}
	return card, nil
}

// encodeImportCard serializes an imported card like the REST write path
// (contact.encodeCard): go-vcard's encoding with CATEGORIES list separators
// restored.
func encodeImportCard(card vcard.Card) (string, error) {
	var buf strings.Builder
	if err := vcard.NewEncoder(&buf).Encode(card); err != nil {
		return "", err
	}
	return addressbook.RestoreVCardCommaLists(buf.String()), nil
}

// Denormalized field derivation lives on AddressObject — see
// PopulateDenormFieldsFromVCard and ExtractDenormFieldsFromCard in
// domain/addressbook. All write paths funnel through those.
