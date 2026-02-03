package importexport

// ImportOptions defines options for import operations
type ImportOptions struct {
	DuplicateHandling string // "skip", "replace", "duplicate"
}

// ImportResult represents the result of an import operation
type ImportResult struct {
	Total    int           `json:"total"`
	Imported int           `json:"imported"`
	Skipped  int           `json:"skipped"`
	Failed   int           `json:"failed"`
	Errors   []ImportError `json:"errors,omitempty"`
}

// ImportError represents an error during import of a single item
type ImportError struct {
	Index   int    `json:"index"`
	UID     string `json:"uid,omitempty"`
	Summary string `json:"summary,omitempty"`
	Error   string `json:"error"`
}

// ExportMetadata represents metadata for a full backup export
type ExportMetadata struct {
	ExportedAt   string                `json:"exported_at"`
	Version      string                `json:"version"`
	Calendars    []CalendarMetadata    `json:"calendars"`
	AddressBooks []AddressBookMetadata `json:"addressbooks"`
}

// CalendarMetadata represents calendar metadata in the export
type CalendarMetadata struct {
	Name       string `json:"name"`
	Color      string `json:"color,omitempty"`
	Timezone   string `json:"timezone,omitempty"`
	EventCount int    `json:"event_count"`
}

// AddressBookMetadata represents address book metadata in the export
type AddressBookMetadata struct {
	Name         string `json:"name"`
	ContactCount int    `json:"contact_count"`
}
