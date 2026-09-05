package toml

import "time"

// LocalDate represents a TOML local date without a timezone.
type LocalDate struct {
	time.Time
}

// LocalTime represents a TOML local time without a date or timezone.
type LocalTime struct {
	time.Time
}

// LocalDateTime represents a TOML local date-time without a timezone.
type LocalDateTime struct {
	time.Time
}
