package store

import "context"

// SeedStaff inserts the cast once, at start-up.
//
// Shared across every session and never copied: what belongs to a visitor is the
// work, not the people. Two visitors both signing in as Dana are the same Dana
// looking at different claims.
//
// Idempotent, so it runs on every boot without a guard.
func (s *Store) SeedStaff(ctx context.Context) error {
	cast := []Staff{
		{Reference: "user:ines", Name: "Inés Moreau", Title: "Intake handler",
			Groups: []string{"group:intake"}},
		{Reference: "user:dana", Name: "Dana Whitfield", Title: "Claims adjuster",
			Groups: []string{"group:adjusters"}},
		{Reference: "user:marek", Name: "Marek Sobczak", Title: "Fraud investigator",
			Groups: []string{"group:siu"}},
		{Reference: "user:priya", Name: "Priya Raman", Title: "Underwriter",
			Groups: []string{"group:underwriters"}},
		{Reference: "user:tom", Name: "Tom Bexley", Title: "Senior underwriter",
			Groups: []string{"group:underwriters", "group:senior-uw"}},
	}

	for order, member := range cast {
		_, err := s.pool.Exec(ctx,
			`insert into console.staff (reference, name, title, groups, sort_order)
			 values ($1, $2, $3, $4, $5)
			 on conflict (reference) do update
			 set name = excluded.name, title = excluded.title,
			     groups = excluded.groups, sort_order = excluded.sort_order`,
			member.Reference, member.Name, member.Title, member.Groups, order)
		if err != nil {
			return err
		}
	}

	return nil
}
