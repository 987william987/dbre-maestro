package model

import "testing"

func TestNormalizeMySQLRollbackEngine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "my2sql", in: MySQLRollbackEngineMy2SQL, want: MySQLRollbackEngineMy2SQL},
		{name: "prior backup", in: MySQLRollbackEnginePriorBackup, want: MySQLRollbackEnginePriorBackup},
		{name: "hybrid", in: MySQLRollbackEngineHybrid, want: MySQLRollbackEngineHybrid},
		{name: "unknown defaults to hybrid", in: "unknown", want: MySQLRollbackEngineHybrid},
		{name: "empty defaults to hybrid", in: "", want: MySQLRollbackEngineHybrid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeMySQLRollbackEngine(tt.in); got != tt.want {
				t.Fatalf("NormalizeMySQLRollbackEngine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
