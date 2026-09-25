module github.com/mike-akdeniz/flowcore/client

go 1.25.7

// The client always builds against the working tree rather than a published
// version, so it cannot drift from the library it demonstrates and no release
// has to be tagged to keep it current.
replace github.com/mike-akdeniz/flowcore => ../
