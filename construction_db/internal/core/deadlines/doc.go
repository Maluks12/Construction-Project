// Package deadlines is the deadline engine (Rev 4 §2.3): which of a regime's
// deadline rules apply to a subject, and how stored deadlines change when the
// subject is amended. The dates themselves are computed in Postgres by
// deadline_due (migration 000015), never in Go, so every stored due value
// comes from one implementation and the calendar it reads.
package deadlines
