package tableschema

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

func BuildExport(ctx context.Context, db Queryer, request ExportRequest) (*ExportResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot, err := LoadSnapshot(ctx, db, request.Source.Database, request.Source.Tables)
	if err != nil {
		return nil, err
	}
	dependencies, err := LoadDependencies(ctx, db, request.Source.Database, request.Source.Tables)
	if err != nil {
		return nil, err
	}
	analysis, err := AnalyzeDependencies(request.Source.Database, request.Source.Tables, dependencies)
	if err != nil {
		return nil, err
	}
	if len(analysis.Cycles) > 0 {
		return nil, &StableError{Code: ErrorForeignKeyCycle, Message: "selected tables contain a foreign key cycle"}
	}
	capabilities, err := LoadCapabilities(ctx, db)
	if err != nil {
		return nil, err
	}
	return assembleExport(ctx, snapshot, snapshot.Database, analysis, request.Transformation, capabilities)
}

func assembleExport(ctx context.Context, snapshot *Snapshot, targetDatabase string, analysis DependencyAnalysis, config TransformationConfig, capabilities Capabilities) (*ExportResult, error) {
	byName := make(map[string]TableSnapshot, len(snapshot.Tables))
	for _, table := range snapshot.Tables {
		byName[table.Name] = table
	}
	result := &ExportResult{
		Database: snapshot.Database, Tables: []ExportTable{}, Order: append([]string(nil), analysis.Order...),
		ExternalDependencies: append([]Dependency(nil), analysis.External...), Warnings: []string{},
	}
	if len(analysis.External) > 0 {
		result.Warnings = append(result.Warnings, "Selected tables reference tables outside this export; create those dependencies first.")
	}
	var script strings.Builder
	for _, name := range analysis.Order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		table, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("dependency order references missing table %q", name)
		}
		parsed, err := ParseCreateTable(table.CreateSQL)
		if err != nil {
			return nil, fmt.Errorf("parse create table for %s: %w", name, err)
		}
		transformed, err := TransformCreateTable(parsed, targetDatabase, config, capabilities)
		if err != nil {
			return nil, err
		}
		sqlText := strings.TrimSpace(transformed.SQL)
		sqlText = strings.TrimSuffix(sqlText, ";") + ";"
		if script.Len()+len(sqlText)+2 > MaxExportBytes {
			return nil, &StableError{Code: ErrorExportTooLarge, Message: "generated export exceeds the size limit"}
		}
		if script.Len() > 0 {
			script.WriteString("\n\n")
		}
		script.WriteString(sqlText)
		sum := sha256.Sum256([]byte(table.CreateSQL))
		result.Tables = append(result.Tables, ExportTable{Name: name, Source: transformed.Source, Output: transformed.Output, SourceDDLSHA256: hex.EncodeToString(sum[:])})
	}
	result.Script = script.String() + "\n"
	return result, nil
}
