package addressbook

type ContactPhoto struct {
	AddressObjectID uint   `gorm:"primaryKey"`
	PhotoData       string `gorm:"type:text"` // Base64 encoded string
	PhotoType       string `gorm:"size:20"`   // e.g. "JPEG", "PNG"
}
