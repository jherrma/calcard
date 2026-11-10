package calendar

import "time"

// AlarmAcknowledgement records that a user dismissed one alarm (#169, spec
// §5.2). AlarmKey is the alarm key of §6.7: a hash that already contains the
// trigger instant, so the table stores no second copy of the time and a moved
// alarm (new key) is not acknowledged. There is no foreign key to users; the
// account purge removes the rows and the cleanup job ages them out.
//
// AcknowledgedAt is set by the use case's clock, in UTC. It is deliberately not
// autoCreateTime: GORM would write time.Now() in time.Local, and on SQLite the
// cleanup's `acknowledged_at < ?` would compare TEXT in two offsets.
type AlarmAcknowledgement struct {
	UserID         uint      `gorm:"primaryKey;autoIncrement:false"`
	AlarmKey       string    `gorm:"primaryKey;size:64"`
	AcknowledgedAt time.Time `gorm:"not null;index"`
}

// TableName is alarm_acknowledgements.
func (AlarmAcknowledgement) TableName() string { return "alarm_acknowledgements" }
