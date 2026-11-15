module github.com/narcisbodea/smstunnel-cli

go 1.24.0

require github.com/narcisbodea/smstunnel-go v1.1.0

// Scheduled texts need smstunnel-go 1.1.0, which is not tagged yet: until it is, build against the folder next to
// this one. Before a release: remove this line, run go get github.com/narcisbodea/smstunnel-go@v1.1.0 (it writes
// go.sum) and go mod tidy, and commit. "go install ...@version" refuses a module with a replace, and the CI and
// release workflows have no ../smstunnel-go.
replace github.com/narcisbodea/smstunnel-go => ../smstunnel-go
