package dbx

import (
	"testing"

	"github.com/pfortini/debeasy/internal/store"
)

// openPostgres/openMySQL have branches for: default host/port, TLS, extra params,
// and url-user variations. These don't require a running DB (we never call Ping),
// so exercise them with openDriver to bump coverage on the factory code.

func TestOpenPostgres_ConfigBranches(t *testing.T) {
	cases := []*store.Connection{
		{Kind: "postgres"}, // defaults: empty user, default host/port
		{Kind: "postgres", Host: "h", Port: 5433, Username: "u"}, // user-only
		{Kind: "postgres", Username: "u", Password: "p", SSLMode: "require", Params: "application_name=debeasy&target_session_attrs=read-write"},
		{Kind: "postgres", Params: "notaurl%ZZ"}, // bad params → still opens, just drops them
	}
	for _, c := range cases {
		d, err := openPostgres(c, 1)
		if err != nil {
			t.Errorf("openPostgres(%+v): %v", c, err)
			continue
		}
		_ = d.Close()
	}
}

func TestOpenMySQL_ConfigBranches(t *testing.T) {
	cases := []*store.Connection{
		{Kind: "mysql"},
		{Kind: "mysql", Username: "u", Password: "p", Database: "d"},
		{Kind: "mysql", Params: "charset=utf8mb4&parseTime=false"},
		{Kind: "mysql", Params: "key-only-no-equals"}, // ignored silently
	}
	for _, c := range cases {
		d, err := openMySQL(c, 1)
		if err != nil {
			t.Errorf("openMySQL(%+v): %v", c, err)
			continue
		}
		_ = d.Close()
	}
}
