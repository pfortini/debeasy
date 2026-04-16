package dbx

import (
	"testing"
	"time"
)

func TestNormaliseValue_Time(t *testing.T) {
	now := time.Date(2026, 4, 16, 17, 30, 0, 0, time.UTC)
	got := normaliseValue(now)
	s, ok := got.(string)
	if !ok {
		t.Fatalf("expected string, got %T", got)
	}
	if s != now.Format(time.RFC3339Nano) {
		t.Errorf("time formatted as %q", s)
	}
}

func TestMySQL_Kind_DirectCall(t *testing.T) {
	// mysql.Kind() is only invoked via the Driver interface under the integration
	// build tag; this unit test exercises the method directly so coverage counts it.
	d := &mysqlDriver{}
	if d.Kind() != KindMySQL {
		t.Errorf("Kind = %s", d.Kind())
	}
}
