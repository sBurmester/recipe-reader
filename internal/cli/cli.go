// Package cli holds recipe-reader's commands: one type per command, whose
// fields are its flags and whose Run hands the work at once to the package
// that does it. cmd/recipe-reader assembles them into the kong command tree,
// parses the command line and runs the selected command.
package cli

// BuildVersion is the version stamped into the binary at link time. It is a
// type of its own so kong can bind it for the Run methods that report it.
type BuildVersion string
