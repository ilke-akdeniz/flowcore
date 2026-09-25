package app

// Identity is a person the visitor can act as.
//
// This is the identity half of the boundary, and it is deliberately the dullest
// code here: a hard-coded list. FlowCore has no users, no groups, no membership
// and no authorization. It receives opaque strings and compares them for equality,
// and everything that gives those strings meaning lives in this file.
//
// A real client would resolve these from a directory, an OIDC token, or its own
// tables. The shape of what it hands FlowCore would be identical.
type Identity struct {
	// Reference is what FlowCore records as completedBy, and one of the values it
	// matches assignee_id against. Opaque: the "user:" prefix is this client's
	// convention and means nothing to the library.
	Reference string
	Name      string
	Title     string
	// Groups are this person's memberships, resolved here and passed to the
	// worklist as extra references to match. Deciding whether Dana is in
	// group:security is exactly the question FlowCore refuses to answer, which is
	// why it takes a set of references rather than a user.
	Groups []string
}

// Label is a human name where there is one, and the raw reference otherwise.
//
// An agent completing a step arrives here as a bare reference with no roster
// entry, which is the point: the library takes a string for completedBy and does
// not care whether a person is behind it.
func (i Identity) Label() string {
	if i.Name != "" {
		return i.Name
	}

	return i.Reference
}

// WorklistReferences is what to ask the worklist about: this person, plus every
// group they belong to.
func (i Identity) WorklistReferences() []string {
	references := make([]string, 0, len(i.Groups)+1)
	references = append(references, i.Reference)

	return append(references, i.Groups...)
}

// CanActAs reports whether this identity would find the given assignee in its own
// worklist. It is presentation only — the library does not check it, and a step
// can be completed by anyone. That is deliberate: FlowCore records who acted and
// never decides whether they were allowed to.
func (i Identity) CanActAs(assignee string) bool {
	for _, reference := range i.WorklistReferences() {
		if reference == assignee {
			return true
		}
	}

	return false
}

// Roster is the cast. Their groups line up with the assignees in the seeded
// release workflow, so switching between them moves work in and out of view.
var Roster = []Identity{
	{
		Reference: "user:alex",
		Name:      "Alex Ferrer",
		Title:     "Release author",
		Groups:    nil,
	},
	{
		Reference: "user:dana",
		Name:      "Dana Whitfield",
		Title:     "Security engineer",
		Groups:    []string{"group:security"},
	},
	{
		Reference: "user:priya",
		Name:      "Priya Raman",
		Title:     "QA lead",
		Groups:    []string{"group:qa"},
	},
	{
		Reference: "user:sam",
		Name:      "Sam Okonkwo",
		Title:     "Counsel",
		Groups:    []string{"group:legal"},
	},
}

// IdentityByReference finds a roster member, falling back to the first so a
// tampered cookie cannot produce a session with nobody signed in.
func IdentityByReference(reference string) Identity {
	for _, identity := range Roster {
		if identity.Reference == reference {
			return identity
		}
	}

	return Roster[0]
}
