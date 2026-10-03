package tableschema

import (
	"strings"
	"testing"
)

func testCapabilities() Capabilities {
	return Capabilities{
		Engines: map[string]bool{"innodb": true, "myisam": true},
		Charsets: map[string]map[string]bool{
			"utf8mb4": {"utf8mb4_0900_ai_ci": true, "utf8mb4_unicode_ci": true},
			"latin1":  {"latin1_swedish_ci": true},
		},
	}
}

func TestTransformCreateTablePreservesBodyAndChangesOnlyRequestedOptions(t *testing.T) {
	ddl := "CREATE TABLE `source-db`.`odd``table` (\n" +
		"  `id` bigint NOT NULL AUTO_INCREMENT,\n" +
		"  `note` varchar(255) GENERATED ALWAYS AS (concat('ENGINE=Fake ', `id`)) STORED,\n" +
		"  PRIMARY KEY (`id`)\n" +
		") ENGINE=InnoDB AUTO_INCREMENT=42 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci ROW_FORMAT=DYNAMIC COMMENT='AUTO_INCREMENT=900 ENGINE=Nope'"
	parsed, err := ParseCreateTable(ddl)
	if err != nil {
		t.Fatal(err)
	}
	result, err := TransformCreateTable(parsed, "target-db", TransformationConfig{
		ResetAutoIncrement: true,
		Engine:             "MyISAM", Charset: "latin1", Collation: "latin1_swedish_ci", RowFormat: "COMPACT",
	}, testCapabilities())
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"CREATE TABLE `target-db`.`odd``table`", "ENGINE=MyISAM", "DEFAULT CHARSET=latin1",
		"COLLATE=latin1_swedish_ci", "ROW_FORMAT=COMPACT", "concat('ENGINE=Fake ', `id`)",
		"COMMENT='AUTO_INCREMENT=900 ENGINE=Nope'",
	} {
		if !strings.Contains(result.SQL, want) {
			t.Fatalf("transformed SQL missing %q:\n%s", want, result.SQL)
		}
	}
	if strings.Contains(result.SQL, "AUTO_INCREMENT=42") {
		t.Fatalf("table AUTO_INCREMENT was not reset:\n%s", result.SQL)
	}
	if result.Output.AutoIncrement != "" || result.Source.AutoIncrement != "42" {
		t.Fatalf("unexpected options: %#v", result)
	}
}

func TestTransformCreateTableAddsMissingOptionsBeforePartitionClause(t *testing.T) {
	ddl := "CREATE TABLE t (`id` int NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4\nPARTITION BY HASH (`id`) PARTITIONS 4"
	parsed, err := ParseCreateTable(ddl)
	if err != nil {
		t.Fatal(err)
	}
	result, err := TransformCreateTable(parsed, "archive", TransformationConfig{Collation: "utf8mb4_unicode_ci", RowFormat: "DYNAMIC"}, testCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	partition := strings.Index(result.SQL, "PARTITION BY")
	if partition < 0 || strings.Index(result.SQL, "COLLATE=utf8mb4_unicode_ci") > partition || strings.Index(result.SQL, "ROW_FORMAT=DYNAMIC") > partition {
		t.Fatalf("new table options must be inserted before partition clause:\n%s", result.SQL)
	}
}

func TestParseCreateTableSupportsMySQL57And80ShowCreateFixtures(t *testing.T) {
	fixtures := []struct {
		name string
		ddl  string
		want TableOptions
	}{
		{
			name: "mysql57",
			ddl:  "CREATE TABLE `legacy_orders` (\n  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,\n  `parent_id` bigint(20) unsigned DEFAULT NULL,\n  PRIMARY KEY (`id`),\n  KEY `idx_parent` (`parent_id`),\n  CONSTRAINT `fk_parent` FOREIGN KEY (`parent_id`) REFERENCES `parents` (`id`)\n) ENGINE=InnoDB AUTO_INCREMENT=812 DEFAULT CHARSET=utf8mb4 ROW_FORMAT=COMPRESSED COMMENT='legacy table'",
			want: TableOptions{AutoIncrement: "812", Engine: "InnoDB", Charset: "utf8mb4", RowFormat: "COMPRESSED"},
		},
		{
			name: "mysql80",
			ddl:  "CREATE TABLE `orders` (\n  `id` bigint unsigned NOT NULL AUTO_INCREMENT,\n  `payload` json DEFAULT NULL,\n  `search_key` varchar(255) GENERATED ALWAYS AS (json_unquote(json_extract(`payload`,_utf8mb4'$.key'))) STORED,\n  PRIMARY KEY (`id`)\n) ENGINE=InnoDB AUTO_INCREMENT=99 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci ROW_FORMAT=DYNAMIC /*!50100 PARTITION BY HASH (`id`) PARTITIONS 4 */",
			want: TableOptions{AutoIncrement: "99", Engine: "InnoDB", Charset: "utf8mb4", Collation: "utf8mb4_0900_ai_ci", RowFormat: "DYNAMIC"},
		},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			parsed, err := ParseCreateTable(fixture.ddl)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Options != fixture.want {
				t.Fatalf("options = %#v, want %#v", parsed.Options, fixture.want)
			}
			result, err := TransformCreateTable(parsed, "target", TransformationConfig{}, testCapabilities())
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(result.SQL, "CREATE TABLE `target`."+QuoteIdentifier(parsed.TableName)) {
				t.Fatalf("target identifier not rewritten: %s", result.SQL)
			}
			if fixture.name == "mysql80" {
				withOption, err := TransformCreateTable(parsed, "target", TransformationConfig{RowFormat: "COMPACT"}, testCapabilities())
				if err != nil {
					t.Fatal(err)
				}
				if strings.Index(withOption.SQL, "ROW_FORMAT=COMPACT") > strings.Index(withOption.SQL, "/*!50100 PARTITION") {
					t.Fatalf("option was inserted after executable partition comment: %s", withOption.SQL)
				}
			}
		})
	}
}

func TestTransformCreateTablePreservesSourceSpecificRowFormatWhenNotOverridden(t *testing.T) {
	parsed, err := ParseCreateTable("CREATE TABLE t (`id` int) ENGINE=MyISAM DEFAULT CHARSET=latin1 COLLATE=latin1_swedish_ci ROW_FORMAT=FIXED")
	if err != nil {
		t.Fatal(err)
	}
	result, err := TransformCreateTable(parsed, "target", TransformationConfig{}, testCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	if result.Output.RowFormat != "FIXED" || !strings.Contains(result.SQL, "ROW_FORMAT=FIXED") {
		t.Fatalf("source row format was not preserved: %#v", result)
	}
}

func TestTransformCreateTableRejectsUnsupportedOrMismatchedOptions(t *testing.T) {
	parsed, err := ParseCreateTable("CREATE TABLE t (`id` int) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci")
	if err != nil {
		t.Fatal(err)
	}
	tests := []TransformationConfig{
		{Engine: "ARCHIVE"},
		{Charset: "latin1"},
		{RowFormat: "MAGIC"},
	}
	for _, config := range tests {
		_, err := TransformCreateTable(parsed, "target", config, testCapabilities())
		stable, ok := err.(*StableError)
		if !ok || stable.Code != ErrorInvalidTransformation {
			t.Fatalf("config %#v error = %#v", config, err)
		}
	}
}

func TestTransformCreateTableDoesNotMutateParsedInputAndIsDeterministic(t *testing.T) {
	ddl := "CREATE TABLE `t` (`id` int, `v` varchar(10) COMMENT 'ROW_FORMAT=X') ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci"
	parsed, err := ParseCreateTable(ddl)
	if err != nil {
		t.Fatal(err)
	}
	first, err := TransformCreateTable(parsed, "target", TransformationConfig{RowFormat: "DYNAMIC"}, testCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	second, err := TransformCreateTable(parsed, "target", TransformationConfig{RowFormat: "DYNAMIC"}, testCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	if first.SQL != second.SQL {
		t.Fatalf("non-deterministic output:\n%s\n%s", first.SQL, second.SQL)
	}
	if parsed.SQL != ddl {
		t.Fatal("transform mutated parsed input")
	}
}

func TestParseCreateTableRejectsNonCreateAndUnterminatedInput(t *testing.T) {
	for _, ddl := range []string{"ALTER TABLE t ADD c int", "CREATE TABLE `", "CREATE TABLE `broken (`id` int)", "CREATE TABLE t (`id` varchar(10) COMMENT 'broken)"} {
		if _, err := ParseCreateTable(ddl); err == nil {
			t.Fatalf("ParseCreateTable(%q) succeeded", ddl)
		}
	}
}

func FuzzTransformCreateTableNeverPanicsOrRewritesQuotedContent(f *testing.F) {
	f.Add("plain text")
	f.Add("ENGINE=Fake AUTO_INCREMENT=123")
	f.Add("quote ' and backtick ` and comment /*")
	f.Fuzz(func(t *testing.T, content string) {
		content = strings.ReplaceAll(content, "\\", "\\\\")
		content = strings.ReplaceAll(content, "'", "''")
		ddl := "CREATE TABLE `t` (`v` varchar(255) COMMENT '" + content + "') ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci"
		parsed, err := ParseCreateTable(ddl)
		if err != nil {
			return
		}
		result, err := TransformCreateTable(parsed, "target", TransformationConfig{RowFormat: "DYNAMIC"}, testCapabilities())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result.SQL, "COMMENT '"+content+"'") {
			t.Fatalf("quoted content changed:\n%s", result.SQL)
		}
	})
}
