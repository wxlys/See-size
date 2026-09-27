package secretflag

import (
	"bytes"
	"flag"
	"strings"
	"testing"
)

func TestEnvironment(t *testing.T) {
	for _, args := range [][]string{nil, {"-token=override"}, {"-token="}} {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		v := fs.String("token", "", "credential")
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		Environment(fs, "token", v, func(string) string { return "secret-sentinel" }, "TOKEN")
		want := "secret-sentinel"
		if len(args) > 0 {
			want = strings.TrimPrefix(args[0], "-token=")
		}
		if *v != want {
			t.Fatalf("got %q want %q", *v, want)
		}
		var out bytes.Buffer
		fs.SetOutput(&out)
		fs.PrintDefaults()
		if strings.Contains(out.String(), "secret-sentinel") {
			t.Fatal("secret in usage")
		}
	}
}
