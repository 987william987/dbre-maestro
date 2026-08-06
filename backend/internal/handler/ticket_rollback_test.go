package handler

import "testing"

func TestExtractMy2SQLStatementsSkipsStatsOutput(t *testing.T) {
	raw := `
binlog              starttime            stoptime             startpos   stoppos    rows

binlog              starttime            stoptime             startpos   stoppos    inserts   updates   deletes   database   table
mysql-bin.000001    2026-08-06_08:07:08  2026-08-06_08:07:08  20558018   20558113   0         0         1         william    test_n

INSERT INTO ` + "`william`.`test_n` (`id`) VALUES (1);" + `
`

	got := extractMy2SQLStatements(raw)
	want := "INSERT INTO `william`.`test_n` (`id`) VALUES (1);"
	if got != want {
		t.Fatalf("unexpected rollback sql\nwant: %q\n got: %q", want, got)
	}
}
