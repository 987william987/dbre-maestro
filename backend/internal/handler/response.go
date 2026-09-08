package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/dbre-maestro/maestro/internal/sqlparse"
)

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonCreated(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(v)
}

func jsonErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func readOnlySQLErrorMessage(err error) string {
	var syntaxErr *sqlparse.SyntaxError
	if errors.As(err, &syntaxErr) {
		if syntaxErr.StatementSeq > 0 {
			return fmt.Sprintf("SQL statement %d could not be parsed. It may use syntax that is not supported by the platform parser.", syntaxErr.StatementSeq)
		}
		return "SQL could not be parsed. It may use syntax that is not supported by the platform parser."
	}
	return "only read-only SQL is allowed: " + err.Error()
}

func bindJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
