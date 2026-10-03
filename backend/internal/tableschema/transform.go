package tableschema

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

type TableOptions struct {
	AutoIncrement string `json:"auto_increment,omitempty"`
	Engine        string `json:"engine,omitempty"`
	Charset       string `json:"charset,omitempty"`
	Collation     string `json:"collation,omitempty"`
	RowFormat     string `json:"row_format,omitempty"`
}

type Capabilities struct {
	Engines  map[string]bool
	Charsets map[string]map[string]bool
}

type ParsedCreateTable struct {
	SQL             string
	TableName       string
	NameStart       int
	NameEnd         int
	OptionsInsertAt int
	Options         TableOptions
	optionValues    map[string]span
	optionStarts    map[string]int
}

type TransformResult struct {
	SQL    string       `json:"sql"`
	Source TableOptions `json:"source"`
	Output TableOptions `json:"output"`
}

type span struct{ start, end int }
type token struct {
	text       string
	start, end int
	kind       byte
}

func ParseCreateTable(sqlText string) (*ParsedCreateTable, error) {
	tokens, err := lexSQL(sqlText)
	if err != nil {
		return nil, err
	}
	if len(tokens) < 4 || !equalWord(tokens[0], "CREATE") || !equalWord(tokens[1], "TABLE") {
		return nil, fmt.Errorf("expected CREATE TABLE statement")
	}
	i := 2
	if i+2 < len(tokens) && equalWord(tokens[i], "IF") && equalWord(tokens[i+1], "NOT") && equalWord(tokens[i+2], "EXISTS") {
		i += 3
	}
	if i >= len(tokens) || !isIdentifier(tokens[i]) {
		return nil, fmt.Errorf("missing table identifier")
	}
	nameStart, nameEnd := tokens[i].start, tokens[i].end
	tableName := unquoteIdentifier(tokens[i].text)
	i++
	if i+1 < len(tokens) && tokens[i].text == "." && isIdentifier(tokens[i+1]) {
		nameEnd = tokens[i+1].end
		tableName = unquoteIdentifier(tokens[i+1].text)
		i += 2
	}
	if i >= len(tokens) || tokens[i].text != "(" {
		return nil, fmt.Errorf("missing table definition")
	}
	depth, closeAt := 0, -1
	for ; i < len(tokens); i++ {
		switch tokens[i].text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				closeAt = tokens[i].end
				i++
				goto bodyDone
			}
		}
	}
bodyDone:
	if closeAt < 0 {
		return nil, fmt.Errorf("unterminated table definition")
	}
	partitionAt := len(sqlText)
	for j := i; j < len(tokens); j++ {
		if equalWord(tokens[j], "PARTITION") {
			partitionAt = tokens[j].start
			break
		}
	}
	if executablePartitionAt := versionedPartitionStart(sqlText, closeAt); executablePartitionAt >= 0 && executablePartitionAt < partitionAt {
		partitionAt = executablePartitionAt
	}
	parsed := &ParsedCreateTable{SQL: sqlText, TableName: tableName, NameStart: nameStart, NameEnd: nameEnd, OptionsInsertAt: partitionAt, optionValues: map[string]span{}, optionStarts: map[string]int{}}
	parseTableOptions(tokens[i:], partitionAt, parsed)
	return parsed, nil
}

func parseTableOptions(tokens []token, limit int, parsed *ParsedCreateTable) {
	for i := 0; i < len(tokens) && tokens[i].start < limit; i++ {
		key, valueIndex := "", -1
		switch {
		case equalWord(tokens[i], "ENGINE"):
			key, valueIndex = "engine", optionValueIndex(tokens, i+1, limit)
		case equalWord(tokens[i], "AUTO_INCREMENT"):
			key, valueIndex = "auto_increment", optionValueIndex(tokens, i+1, limit)
		case equalWord(tokens[i], "COLLATE"):
			key, valueIndex = "collation", optionValueIndex(tokens, i+1, limit)
		case equalWord(tokens[i], "ROW_FORMAT"):
			key, valueIndex = "row_format", optionValueIndex(tokens, i+1, limit)
		case equalWord(tokens[i], "CHARSET"):
			key, valueIndex = "charset", optionValueIndex(tokens, i+1, limit)
		case equalWord(tokens[i], "DEFAULT") && i+1 < len(tokens) && equalWord(tokens[i+1], "CHARSET"):
			key, valueIndex = "charset", optionValueIndex(tokens, i+2, limit)
		case equalWord(tokens[i], "DEFAULT") && i+2 < len(tokens) && equalWord(tokens[i+1], "CHARACTER") && equalWord(tokens[i+2], "SET"):
			key, valueIndex = "charset", optionValueIndex(tokens, i+3, limit)
		}
		if key == "" || valueIndex < 0 || valueIndex >= len(tokens) || tokens[valueIndex].start >= limit {
			continue
		}
		value := unquoteIdentifier(tokens[valueIndex].text)
		parsed.optionValues[key] = span{tokens[valueIndex].start, tokens[valueIndex].end}
		parsed.optionStarts[key] = tokens[i].start
		switch key {
		case "engine":
			parsed.Options.Engine = value
		case "auto_increment":
			parsed.Options.AutoIncrement = value
		case "charset":
			parsed.Options.Charset = value
		case "collation":
			parsed.Options.Collation = value
		case "row_format":
			parsed.Options.RowFormat = value
		}
	}
}

func TransformCreateTable(parsed *ParsedCreateTable, targetDatabase string, config TransformationConfig, capabilities Capabilities) (TransformResult, error) {
	if parsed == nil || parsed.SQL == "" {
		return TransformResult{}, fmt.Errorf("create table statement is required")
	}
	if err := validateIdentifier(targetDatabase); err != nil {
		return TransformResult{}, &StableError{Code: ErrorInvalidTransformation, Message: "invalid target database"}
	}
	output, err := resolveOptions(parsed.Options, config, capabilities)
	if err != nil {
		return TransformResult{}, err
	}
	edits := []edit{{parsed.NameStart, parsed.NameEnd, QuoteIdentifier(targetDatabase) + "." + QuoteIdentifier(parsed.TableName)}}
	setOptionEdit := func(key, value, clause string) {
		if existing, ok := parsed.optionValues[key]; ok {
			edits = append(edits, edit{existing.start, existing.end, value})
		} else if value != "" {
			edits = append(edits, edit{parsed.OptionsInsertAt, parsed.OptionsInsertAt, " " + clause + "=" + value})
		}
	}
	if config.ResetAutoIncrement {
		if value, ok := parsed.optionValues["auto_increment"]; ok {
			start := parsed.optionStarts["auto_increment"]
			for start > 0 && unicode.IsSpace(rune(parsed.SQL[start-1])) {
				start--
			}
			edits = append(edits, edit{start, value.end, ""})
		}
	}
	if config.RowFormat != "" {
		setOptionEdit("row_format", output.RowFormat, "ROW_FORMAT")
	}
	if config.Collation != "" {
		setOptionEdit("collation", output.Collation, "COLLATE")
	}
	if config.Charset != "" {
		setOptionEdit("charset", output.Charset, "DEFAULT CHARSET")
	}
	if config.Engine != "" {
		setOptionEdit("engine", output.Engine, "ENGINE")
	}
	return TransformResult{SQL: applyEdits(parsed.SQL, edits), Source: parsed.Options, Output: output}, nil
}

func resolveOptions(source TableOptions, config TransformationConfig, capabilities Capabilities) (TableOptions, error) {
	output := source
	if config.ResetAutoIncrement {
		output.AutoIncrement = ""
	}
	if config.Engine != "" {
		output.Engine = config.Engine
	}
	if config.Charset != "" {
		output.Charset = config.Charset
	}
	if config.Collation != "" {
		output.Collation = config.Collation
	}
	if config.RowFormat != "" {
		output.RowFormat = strings.ToUpper(config.RowFormat)
	}
	if output.Engine != "" && !capabilities.Engines[strings.ToLower(output.Engine)] {
		return TableOptions{}, invalidTransformation("engine is not supported by the selected server")
	}
	collations, charsetOK := capabilities.Charsets[strings.ToLower(output.Charset)]
	if output.Charset != "" && !charsetOK {
		return TableOptions{}, invalidTransformation("charset is not supported by the selected server")
	}
	if output.Collation != "" && (!charsetOK || !collations[strings.ToLower(output.Collation)]) {
		return TableOptions{}, invalidTransformation("collation does not belong to the selected charset")
	}
	if config.RowFormat != "" {
		allowed := map[string]bool{"DEFAULT": true, "DYNAMIC": true, "COMPACT": true, "COMPRESSED": true, "REDUNDANT": true}
		if !allowed[strings.ToUpper(output.RowFormat)] {
			return TableOptions{}, invalidTransformation("row format is not supported")
		}
	}
	return output, nil
}

func versionedPartitionStart(sqlText string, after int) int {
	upper := strings.ToUpper(sqlText)
	for offset := after; offset < len(sqlText); {
		relativeStart := strings.Index(upper[offset:], "/*!")
		if relativeStart < 0 {
			return -1
		}
		start := offset + relativeStart
		relativeEnd := strings.Index(upper[start+3:], "*/")
		if relativeEnd < 0 {
			return -1
		}
		end := start + 3 + relativeEnd
		if strings.Contains(upper[start+3:end], "PARTITION") {
			return start
		}
		offset = end + 2
	}
	return -1
}

func invalidTransformation(message string) error {
	return &StableError{Code: ErrorInvalidTransformation, Message: message}
}

type edit struct {
	start, end  int
	replacement string
}

func applyEdits(input string, edits []edit) string {
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, item := range edits {
		input = input[:item.start] + item.replacement + input[item.end:]
	}
	return input
}

func optionValueIndex(tokens []token, start, limit int) int {
	if start < len(tokens) && tokens[start].text == "=" {
		start++
	}
	if start < len(tokens) && tokens[start].start < limit {
		return start
	}
	return -1
}

func equalWord(item token, word string) bool {
	return item.kind == 'w' && strings.EqualFold(item.text, word)
}
func isIdentifier(item token) bool { return item.kind == 'w' || item.kind == 'i' }
func unquoteIdentifier(value string) string {
	if len(value) >= 2 && value[0] == '`' {
		return strings.ReplaceAll(value[1:len(value)-1], "``", "`")
	}
	return value
}

func lexSQL(input string) ([]token, error) {
	result := make([]token, 0, 64)
	for i := 0; i < len(input); {
		if unicode.IsSpace(rune(input[i])) {
			i++
			continue
		}
		if input[i] == '#' || (input[i] == '-' && i+2 < len(input) && input[i+1] == '-' && unicode.IsSpace(rune(input[i+2]))) {
			for i < len(input) && input[i] != '\n' {
				i++
			}
			continue
		}
		if input[i] == '/' && i+1 < len(input) && input[i+1] == '*' {
			end := strings.Index(input[i+2:], "*/")
			if end < 0 {
				return nil, fmt.Errorf("unterminated comment")
			}
			i += end + 4
			continue
		}
		start := i
		switch input[i] {
		case '`':
			i++
			closed := false
			for i < len(input) {
				if input[i] == '`' {
					if i+1 < len(input) && input[i+1] == '`' {
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, fmt.Errorf("unterminated quoted identifier")
			}
			result = append(result, token{input[start:i], start, i, 'i'})
		case '\'', '"':
			quote := input[i]
			i++
			closed := false
			for i < len(input) {
				if input[i] == '\\' {
					i += 2
					continue
				}
				if input[i] == quote {
					if i+1 < len(input) && input[i+1] == quote {
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, fmt.Errorf("unterminated string")
			}
			result = append(result, token{input[start:i], start, i, 's'})
		default:
			if isWordByte(input[i]) {
				for i < len(input) && isWordByte(input[i]) {
					i++
				}
				result = append(result, token{input[start:i], start, i, 'w'})
			} else {
				i++
				result = append(result, token{input[start:i], start, i, 'p'})
			}
		}
	}
	return result, nil
}

func isWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '_' || value == '$'
}
