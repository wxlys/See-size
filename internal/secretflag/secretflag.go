// Package secretflag keeps environment credentials out of flag usage defaults.
package secretflag

import "flag"

// Environment applies a fallback only when the flag was not explicitly supplied.
// Call after Parse, so help and parse errors never see environment secrets.
func Environment(fs *flag.FlagSet, name string, value *string, lookup func(string) string, key string) {
	explicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			explicit = true
		}
	})
	if !explicit {
		*value = lookup(key)
	}
}
