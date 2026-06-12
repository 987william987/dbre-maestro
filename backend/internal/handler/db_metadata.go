package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
)

type DBMetadataHandler struct {
	repo    *repository.DBMetadataRepo
	dbConns *repository.DBConnectionRepo
}

func NewDBMetadataHandler(repo *repository.DBMetadataRepo, dbConns *repository.DBConnectionRepo) *DBMetadataHandler {
	return &DBMetadataHandler{repo: repo, dbConns: dbConns}
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
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 500)

	items, err := h.repo.ListObjectSnapshots(r.Context(), connectionID, limit)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list object snapshots failed")
		return
	}

	jsonOK(w, map[string]any{
		"items": items,
		"total": len(items),
	})
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
