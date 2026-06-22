package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
)

type DBMetadataHandler struct {
	repo     *repository.DBMetadataRepo
	dbConns  *repository.DBConnectionRepo
	settings *repository.SettingsRepo
}

func NewDBMetadataHandler(repo *repository.DBMetadataRepo, dbConns *repository.DBConnectionRepo, settings *repository.SettingsRepo) *DBMetadataHandler {
	return &DBMetadataHandler{repo: repo, dbConns: dbConns, settings: settings}
}

func (h *DBMetadataHandler) ListInventory(w http.ResponseWriter, r *http.Request) {
	engine := strings.TrimSpace(r.URL.Query().Get("engine"))
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 200)

	items, err := h.repo.ListInventorySnapshots(r.Context(), engine, limit)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list inventory snapshots failed")
		return
	}

	connections, err := h.dbConns.List(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load db connections failed")
		return
	}

	for i := range items {
		items[i].MappingStatus, items[i].MappingConnections = mapInventorySnapshot(items[i], connections)
	}

	jsonOK(w, map[string]any{
		"items": items,
		"total": len(items),
	})
}

func (h *DBMetadataHandler) ListObjects(w http.ResponseWriter, r *http.Request) {
	connectionID := parsePositiveUint64(r.URL.Query().Get("db_connection_id"))
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 0)

	items, err := h.repo.ListObjectSnapshots(r.Context(), connectionID, limit)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list object snapshots failed")
		return
	}

	allowedConnections, err := h.allowedObjectSnapshotConnections(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load object scan scope failed")
		return
	}
	allowedConnectionIDs := connectionIDSet(allowedConnections)
	items = filterObjectSnapshotsByConnectionIDs(items, allowedConnectionIDs)

	jsonOK(w, map[string]any{
		"items":              items,
		"total":              len(items),
		"connection_options": objectSnapshotConnectionOptions(allowedConnections),
	})
}

func (h *DBMetadataHandler) allowedObjectSnapshotConnections(ctx context.Context) ([]model.DBConnection, error) {
	settings, err := h.settings.Get(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.DBMetadataObjectEnabled {
		return []model.DBConnection{}, nil
	}

	selectedIDs := normalizeAllowedConnectionIDs(settings.DBMetadataObjectEnabledConnectionIDs)
	if len(selectedIDs) == 0 {
		return []model.DBConnection{}, nil
	}

	connections, err := h.dbConns.List(ctx)
	if err != nil {
		return nil, err
	}

	allowed := make([]model.DBConnection, 0, len(selectedIDs))
	for _, conn := range connections {
		if _, ok := selectedIDs[conn.ID]; !ok {
			continue
		}
		if !isSupportedObjectSnapshotDBType(conn.DBType) {
			continue
		}
		allowed = append(allowed, conn)
	}
	return allowed, nil
}

func normalizeAllowedConnectionIDs(ids []uint64) map[uint64]struct{} {
	allowed := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		allowed[id] = struct{}{}
	}
	return allowed
}

func isSupportedObjectSnapshotDBType(dbType string) bool {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "mysql", "postgres", "postgresql":
		return true
	default:
		return false
	}
}

func filterObjectSnapshotsByConnectionIDs(items []model.DBObjectSnapshot, allowed map[uint64]struct{}) []model.DBObjectSnapshot {
	if len(items) == 0 || len(allowed) == 0 {
		return []model.DBObjectSnapshot{}
	}

	filtered := make([]model.DBObjectSnapshot, 0, len(items))
	for _, item := range items {
		if _, ok := allowed[item.DBConnectionID]; !ok {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func connectionIDSet(connections []model.DBConnection) map[uint64]struct{} {
	allowed := make(map[uint64]struct{}, len(connections))
	for _, conn := range connections {
		allowed[conn.ID] = struct{}{}
	}
	return allowed
}

type objectSnapshotConnectionOption struct {
	ID     uint64 `json:"id"`
	Name   string `json:"name"`
	DBType string `json:"db_type"`
}

func objectSnapshotConnectionOptions(connections []model.DBConnection) []objectSnapshotConnectionOption {
	options := make([]objectSnapshotConnectionOption, 0, len(connections))
	for _, conn := range connections {
		options = append(options, objectSnapshotConnectionOption{
			ID:     conn.ID,
			Name:   conn.Name,
			DBType: conn.DBType,
		})
	}
	return options
}

func mapInventorySnapshot(snapshot model.CloudDBInventorySnapshot, connections []model.DBConnection) (string, []string) {
	endpoints := []string{
		trimStringPointer(snapshot.ClusterEndpoint),
		trimStringPointer(snapshot.ClusterReaderEndpoint),
		trimStringPointer(snapshot.InstanceEndpoint),
	}
	matches := []string{}
	for _, conn := range connections {
		host := strings.TrimSpace(conn.Host)
		if host == "" {
			continue
		}
		for _, endpoint := range endpoints {
			if endpoint != "" && endpoint == host {
				matches = append(matches, conn.Name)
				break
			}
		}
	}
	switch len(matches) {
	case 0:
		return "unmatched", nil
	case 1:
		return "matched", matches
	default:
		return "ambiguous", matches
	}
}

func trimStringPointer(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func parsePositiveInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func parsePositiveUint64(raw string) uint64 {
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0
	}
	return value
}
