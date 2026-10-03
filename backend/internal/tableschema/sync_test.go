package tableschema

import (
	"context"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestSyncPreviewStoreExpiresBindsActorAndIsSingleUse(t *testing.T) {
	store := NewSyncPreviewStore()
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	token, expires, err := store.Issue(7, "fingerprint")
	if err != nil || expires != now.Add(PreviewTTL) {
		t.Fatalf("issue = %q %v %v", token, expires, err)
	}
	if err := store.Consume(token, 8, "fingerprint"); stableCode(err) != ErrorPreviewReplayed {
		t.Fatalf("actor substitution = %v", err)
	}
	if err := store.Consume(token, 7, "fingerprint"); stableCode(err) != ErrorPreviewReplayed {
		t.Fatalf("consumed token reuse = %v", err)
	}
	token, _, _ = store.Issue(7, "fingerprint")
	if err := store.Consume(token, 7, "changed"); stableCode(err) != ErrorSourceDDLChanged {
		t.Fatalf("drift = %v", err)
	}
	token, _, _ = store.Issue(7, "fingerprint")
	now = now.Add(PreviewTTL)
	if err := store.Consume(token, 7, "fingerprint"); stableCode(err) != ErrorPreviewExpired {
		t.Fatalf("expiry = %v", err)
	}
}

func TestSyncTargetPreflightRejectsExistingTableBeforeExternalChecks(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("target", "orders").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	err := preflightTarget(context.Background(), db, SyncPreviewRequest{Target: TargetSelection{Database: "target"}}, DependencyAnalysis{Order: []string{"orders"}})
	if stableCode(err) != ErrorTargetTableExists {
		t.Fatalf("error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSyncTargetPreflightRejectsMissingExternalDependency(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("SELECT EXISTS").WithArgs("target", "orders").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("SELECT EXISTS").WithArgs("target", "users").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	request := SyncPreviewRequest{Source: SourceSelection{Database: "source"}, Target: TargetSelection{Database: "target"}}
	analysis := DependencyAnalysis{Order: []string{"orders"}, External: []Dependency{{Database: "source", Table: "orders", ReferencedDatabase: "source", ReferencedTable: "users"}}}
	if err := preflightTarget(context.Background(), db, request, analysis); stableCode(err) != ErrorExternalMissing {
		t.Fatalf("error = %v", err)
	}
}

func TestSyncPreviewFingerprintClassifiesCapabilityAndSourceDrift(t *testing.T) {
	request := SyncPreviewRequest{Source: SourceSelection{ConnectionID: 1, Database: "source", Tables: []string{"orders"}}, Target: TargetSelection{ConnectionID: 2, Database: "target"}}
	snapshot := &Snapshot{Database: "source", Tables: []TableSnapshot{{Name: "orders", CreateSQL: "CREATE TABLE `orders` (`id` bigint)"}}}
	caps := Capabilities{Engines: map[string]bool{"innodb": true}, Charsets: map[string]map[string]bool{}}
	base, _ := syncFingerprint(request, snapshot, nil, caps)
	changedCaps := Capabilities{Engines: map[string]bool{"innodb": true, "myisam": true}, Charsets: map[string]map[string]bool{}}
	capFingerprint, _ := syncFingerprint(request, snapshot, nil, changedCaps)
	store := NewSyncPreviewStore()
	token, _, _ := store.Issue(7, base)
	if err := store.Consume(token, 7, capFingerprint); stableCode(err) != ErrorTargetCapabilityChanged {
		t.Fatalf("capability drift = %v", err)
	}
	changedSnapshot := &Snapshot{Database: "source", Tables: []TableSnapshot{{Name: "orders", CreateSQL: "CREATE TABLE `orders` (`id` varchar(20))"}}}
	sourceFingerprint, _ := syncFingerprint(request, changedSnapshot, nil, caps)
	token, _, _ = store.Issue(7, base)
	if err := store.Consume(token, 7, sourceFingerprint); stableCode(err) != ErrorSourceDDLChanged {
		t.Fatalf("source drift = %v", err)
	}
}

func stableCode(err error) ErrorCode {
	var stable *StableError
	if errors.As(err, &stable) {
		return stable.Code
	}
	return ""
}
