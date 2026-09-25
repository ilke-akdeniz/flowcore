module github.com/mike-akdeniz/flowcore/client

go 1.25.7

// The client always builds against the working tree rather than a published
// version, so it cannot drift from the library it demonstrates and no release
// has to be tagged to keep it current.
replace github.com/mike-akdeniz/flowcore => ../

require (
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.11.0
	github.com/mike-akdeniz/flowcore v0.0.0-00010101000000-000000000000
)

require (
	github.com/anthropics/anthropic-sdk-go v1.75.0 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.2 // indirect
	github.com/invopop/jsonschema v0.14.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/pb33f/ordered-map/v2 v2.3.1 // indirect
	github.com/pressly/goose/v3 v3.27.3 // indirect
	github.com/sethvargo/go-retry v0.4.0 // indirect
	github.com/standard-webhooks/standard-webhooks/libraries v0.0.1 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.yaml.in/yaml/v4 v4.0.0-rc.2 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)
