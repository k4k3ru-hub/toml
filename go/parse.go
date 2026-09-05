package toml

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type parser struct {
	source                []rune
	index                 int
	line                  int
	column                int
	root                  map[string]any
	current               map[string]any
	currentPath           []string
	currentDefinitionPath []string
	defined               map[string]string
}

func parse(data []byte) (map[string]any, error) {
	if !utf8.Valid(data) {
		return nil, &ParseError{Line: 1, Column: 1, Message: "document is not valid UTF-8"}
	}
	for index, character := range []rune(string(data)) {
		if character == '\r' {
			runes := []rune(string(data))
			if index+1 >= len(runes) || runes[index+1] != '\n' {
				return nil, &ParseError{Line: 1, Column: index + 1, Message: "carriage return must be followed by line feed"}
			}
			continue
		}
		if character < 0x20 && character != '\t' && character != '\n' || character == 0x7f {
			return nil, &ParseError{Line: 1, Column: index + 1, Message: "document contains a prohibited control character"}
		}
	}
	p := &parser{
		source:  []rune(strings.TrimPrefix(string(data), "\uFEFF")),
		line:    1,
		column:  1,
		root:    make(map[string]any),
		defined: make(map[string]string),
	}
	p.current = p.root
	if err := p.parseDocument(); err != nil {
		return nil, err
	}
	return p.root, nil
}

func (p *parser) parseDocument() error {
	for {
		p.skipHorizontalWhitespace()
		if p.eof() {
			return nil
		}
		if p.peek() == '#' {
			p.skipComment()
			if err := p.consumeLineEnd(); err != nil {
				return err
			}
			continue
		}
		if p.isNewline() {
			p.consumeNewline()
			continue
		}
		var err error
		if p.peek() == '[' {
			err = p.parseHeader()
		} else {
			err = p.parseKeyValue(p.current, p.currentDefinitionPath, false)
		}
		if err != nil {
			return err
		}
		p.skipHorizontalWhitespace()
		if p.peek() == '#' {
			p.skipComment()
		}
		if err := p.consumeLineEnd(); err != nil {
			return err
		}
	}
}

func (p *parser) parseHeader() error {
	arrayTable := false
	p.advance()
	if p.peek() == '[' {
		arrayTable = true
		p.advance()
	}
	p.skipHorizontalWhitespace()
	path, err := p.parseKeyPath(']')
	if err != nil {
		return err
	}
	p.skipHorizontalWhitespace()
	if p.peek() != ']' {
		return p.errorf("expected closing bracket")
	}
	p.advance()
	if arrayTable {
		if p.peek() != ']' {
			return p.errorf("expected closing array-table bracket")
		}
		p.advance()
	}
	if _, isArray := p.valueAtPath(path).([]any); isArray && !arrayTable {
		return p.errorf("table conflicts with an array table")
	}
	if len(path) == 0 {
		return p.errorf("table name is empty")
	}
	if arrayTable {
		joined := definitionKey(p.arrayDefinitionPath(path))
		if kind := p.defined[joined]; kind != "" && kind != "array-table" {
			return p.errorf("table conflicts with an existing value")
		}
		table, err := p.appendArrayTable(path)
		if err != nil {
			return err
		}
		p.defined[joined] = "array-table"
		p.current = table
		p.currentPath = append([]string(nil), path...)
		p.currentDefinitionPath = p.definitionPath(path)
		return nil
	}
	joined := definitionKey(p.definitionPath(path))
	if p.defined[joined] != "" {
		return p.errorf("table is already defined")
	}
	table, err := p.ensureTable(path)
	if err != nil {
		return err
	}
	p.defined[joined] = "table"
	p.current = table
	p.currentPath = append([]string(nil), path...)
	p.currentDefinitionPath = p.definitionPath(path)
	return nil
}

func (p *parser) parseKeyValue(table map[string]any, prefix []string, inline bool) error {
	path, err := p.parseKeyPath('=')
	if err != nil {
		return err
	}
	p.skipHorizontalWhitespace()
	if p.peek() != '=' {
		return p.errorf("expected equals sign")
	}
	p.advance()
	p.skipHorizontalWhitespace()
	value, err := p.parseValue(inline)
	if err != nil {
		return err
	}
	if len(path) == 0 {
		return p.errorf("key is empty")
	}
	if inline {
		return p.setLocalValue(table, path, value)
	}
	return p.setValue(table, prefix, path, value)
}

func (p *parser) parseKeyPath(stop rune) ([]string, error) {
	var path []string
	expectingKey := true
	for {
		p.skipHorizontalWhitespace()
		if p.eof() || p.peek() == stop {
			if expectingKey && len(path) > 0 {
				return nil, p.errorf("key path has a trailing dot")
			}
			break
		}
		key, err := p.parseKey()
		if err != nil {
			return nil, err
		}
		path = append(path, key)
		expectingKey = false
		p.skipHorizontalWhitespace()
		if p.peek() != '.' {
			break
		}
		p.advance()
		expectingKey = true
	}
	return path, nil
}

func (p *parser) parseKey() (string, error) {
	switch p.peek() {
	case '"':
		return p.parseBasicString(false)
	case '\'':
		return p.parseLiteralString(false)
	}
	start := p.index
	for isBareKeyRune(p.peek()) {
		p.advance()
	}
	if start == p.index {
		return "", p.errorf("invalid key")
	}
	return string(p.source[start:p.index]), nil
}

func (p *parser) parseValue(inline bool) (any, error) {
	if p.eof() {
		return nil, p.errorf("value is missing")
	}
	switch p.peek() {
	case '"':
		return p.parseBasicString(p.hasPrefix("\"\"\""))
	case '\'':
		return p.parseLiteralString(p.hasPrefix("'''"))
	case '[':
		return p.parseArray()
	case '{':
		return p.parseInlineTable()
	}
	start := p.index
	for !p.eof() {
		r := p.peek()
		if r == ',' || r == ']' || r == '}' || r == '#' || p.isNewline() || (inline && (r == '\r' || r == '\n')) {
			break
		}
		p.advance()
	}
	token := strings.TrimSpace(string(p.source[start:p.index]))
	if token == "" {
		return nil, p.errorf("value is missing")
	}
	return p.parseToken(token)
}

func (p *parser) parseToken(token string) (any, error) {
	if token == "true" {
		return true, nil
	}
	if token == "false" {
		return false, nil
	}
	if value, ok := parseDateTime(token); ok {
		return value, nil
	}
	if value, ok := parseInteger(token); ok {
		return value, nil
	}
	if value, ok := parseFloat(token); ok {
		return value, nil
	}
	return nil, p.errorf("invalid value %q", token)
}

func (p *parser) parseBasicString(multiline bool) (string, error) {
	delimiterLength := 1
	if multiline {
		delimiterLength = 3
	}
	for range delimiterLength {
		p.advance()
	}
	if multiline && p.isNewline() {
		p.consumeNewline()
	}
	var result strings.Builder
	for !p.eof() {
		if multiline && p.hasPrefix("\"\"\"") {
			count := p.countConsecutive('"')
			if count > 5 {
				return "", p.errorf("too many quotes at the end of multiline basic string")
			}
			for range count - 3 {
				result.WriteByte('"')
			}
			for range count {
				p.advance()
			}
			return result.String(), nil
		}
		if !multiline && p.peek() == '"' {
			p.advance()
			return result.String(), nil
		}
		if !multiline && p.isNewline() {
			return "", p.errorf("newline in basic string")
		}
		r := p.advance()
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return "", p.errorf("control character in string")
		}
		if r != '\\' {
			result.WriteRune(r)
			continue
		}
		if multiline {
			saved := p.index
			p.skipHorizontalWhitespace()
			if p.isNewline() {
				p.consumeNewline()
				for {
					p.skipHorizontalWhitespace()
					if !p.isNewline() {
						break
					}
					p.consumeNewline()
				}
				continue
			}
			p.index = saved
			p.recalculatePosition()
		}
		escaped, err := p.parseEscape()
		if err != nil {
			return "", err
		}
		result.WriteRune(escaped)
	}
	return "", p.errorf("unterminated basic string")
}

func (p *parser) parseEscape() (rune, error) {
	switch r := p.advance(); r {
	case 'b':
		return '\b', nil
	case 't':
		return '\t', nil
	case 'n':
		return '\n', nil
	case 'f':
		return '\f', nil
	case 'r':
		return '\r', nil
	case '"':
		return '"', nil
	case '\\':
		return '\\', nil
	case 'u':
		return p.parseUnicodeEscape(4)
	case 'U':
		return p.parseUnicodeEscape(8)
	default:
		return 0, p.errorf("invalid string escape")
	}
}

func (p *parser) parseUnicodeEscape(length int) (rune, error) {
	if p.index+length > len(p.source) {
		return 0, p.errorf("incomplete Unicode escape")
	}
	digits := string(p.source[p.index : p.index+length])
	value, err := strconv.ParseUint(digits, 16, 32)
	if err != nil || !utf8.ValidRune(rune(value)) || value >= 0xD800 && value <= 0xDFFF {
		return 0, p.errorf("invalid Unicode escape")
	}
	for range length {
		p.advance()
	}
	return rune(value), nil
}

func (p *parser) parseLiteralString(multiline bool) (string, error) {
	delimiterLength := 1
	if multiline {
		delimiterLength = 3
	}
	for range delimiterLength {
		p.advance()
	}
	if multiline && p.isNewline() {
		p.consumeNewline()
	}
	var result strings.Builder
	for !p.eof() {
		if multiline && p.hasPrefix("'''") {
			count := p.countConsecutive('\'')
			if count > 5 {
				return "", p.errorf("too many quotes at the end of multiline literal string")
			}
			for range count - 3 {
				result.WriteByte('\'')
			}
			for range count {
				p.advance()
			}
			return result.String(), nil
		}
		if !multiline && p.peek() == '\'' {
			p.advance()
			return result.String(), nil
		}
		if !multiline && p.isNewline() {
			return "", p.errorf("newline in literal string")
		}
		r := p.advance()
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return "", p.errorf("control character in string")
		}
		result.WriteRune(r)
	}
	return "", p.errorf("unterminated literal string")
}

func (p *parser) countConsecutive(character rune) int {
	count := 0
	for p.index+count < len(p.source) && p.source[p.index+count] == character {
		count++
	}
	return count
}

func (p *parser) parseArray() ([]any, error) {
	p.advance()
	var values []any
	for {
		p.skipArrayWhitespace()
		if p.peek() == ']' {
			p.advance()
			return values, nil
		}
		value, err := p.parseValue(false)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
		p.skipArrayWhitespace()
		if p.peek() == ']' {
			p.advance()
			return values, nil
		}
		if p.peek() != ',' {
			return nil, p.errorf("expected comma or closing array bracket")
		}
		p.advance()
	}
}

func (p *parser) parseInlineTable() (map[string]any, error) {
	p.advance()
	table := make(map[string]any)
	defined := make(map[string]string)
	p.skipHorizontalWhitespace()
	if p.peek() == '}' {
		p.advance()
		return table, nil
	}
	for {
		if p.isNewline() {
			return nil, p.errorf("newline in inline table")
		}
		path, err := p.parseKeyPath('=')
		if err != nil {
			return nil, err
		}
		p.skipHorizontalWhitespace()
		if p.peek() != '=' {
			return nil, p.errorf("expected equals sign")
		}
		p.advance()
		p.skipHorizontalWhitespace()
		value, err := p.parseValue(true)
		if err != nil {
			return nil, err
		}
		if err := p.setInlineValue(table, path, value, defined); err != nil {
			return nil, err
		}
		p.skipHorizontalWhitespace()
		if p.peek() == '}' {
			p.advance()
			return table, nil
		}
		if p.peek() != ',' {
			return nil, p.errorf("expected comma or closing inline-table brace")
		}
		p.advance()
		p.skipHorizontalWhitespace()
		if p.peek() == '}' {
			return nil, p.errorf("trailing comma in inline table")
		}
	}
}

func (p *parser) setInlineValue(table map[string]any, path []string, value any, defined map[string]string) error {
	if len(path) == 0 {
		return p.errorf("key is empty")
	}
	for index := 1; index < len(path); index++ {
		key := definitionKey(path[:index])
		if defined[key] == "value" {
			return p.errorf("dotted key conflicts with an existing value")
		}
		if defined[key] == "" {
			defined[key] = "dotted"
		}
	}
	key := definitionKey(path)
	if defined[key] != "" {
		return p.errorf("key is already defined")
	}
	if err := p.setLocalValue(table, path, value); err != nil {
		return err
	}
	defined[key] = "value"
	return nil
}

func (p *parser) setValue(table map[string]any, prefix []string, path []string, value any) error {
	absolutePath := append(append([]string(nil), prefix...), path...)
	for index := len(prefix) + 1; index < len(absolutePath); index++ {
		joined := definitionKey(absolutePath[:index])
		if kind := p.defined[joined]; kind != "" && kind != "dotted" {
			return p.errorf("dotted key conflicts with an existing value")
		}
		if p.defined[joined] == "" {
			p.defined[joined] = "dotted"
		}
	}
	joined := definitionKey(absolutePath)
	if p.defined[joined] != "" {
		return p.errorf("key is already defined")
	}
	if err := p.setLocalValue(table, path, value); err != nil {
		return err
	}
	p.defined[joined] = "value"
	return nil
}

func (p *parser) valueAtPath(path []string) any {
	var current any = p.root
	for index, key := range path {
		table, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = table[key]
		if values, ok := current.([]any); ok && len(values) > 0 && index < len(path)-1 {
			current = values[len(values)-1]
		}
	}
	return current
}

func (p *parser) setLocalValue(table map[string]any, path []string, value any) error {
	current := table
	for _, key := range path[:len(path)-1] {
		existing, ok := current[key]
		if !ok {
			next := make(map[string]any)
			current[key] = next
			current = next
			continue
		}
		next, ok := existing.(map[string]any)
		if !ok {
			return p.errorf("dotted key conflicts with an existing value")
		}
		current = next
	}
	key := path[len(path)-1]
	if _, exists := current[key]; exists {
		return p.errorf("key is already defined")
	}
	current[key] = value
	return nil
}

func (p *parser) ensureTable(path []string) (map[string]any, error) {
	current := p.root
	definitionPath := make([]string, 0, len(path))
	for _, key := range path {
		definitionPath = append(definitionPath, key)
		existing, ok := current[key]
		if !ok {
			next := make(map[string]any)
			current[key] = next
			current = next
			continue
		}
		switch value := existing.(type) {
		case map[string]any:
			if p.defined[definitionKey(definitionPath)] == "value" {
				return nil, p.errorf("table conflicts with an existing value")
			}
			current = value
		case []any:
			if p.defined[definitionKey(definitionPath)] == "value" {
				return nil, p.errorf("table conflicts with an array value")
			}
			if len(value) == 0 {
				return nil, p.errorf("array table is empty")
			}
			last, ok := value[len(value)-1].(map[string]any)
			if !ok {
				return nil, p.errorf("table conflicts with an array value")
			}
			definitionPath[len(definitionPath)-1] = arrayElementDefinition(key, len(value)-1)
			current = last
		default:
			return nil, p.errorf("table conflicts with an existing value")
		}
	}
	return current, nil
}

func (p *parser) appendArrayTable(path []string) (map[string]any, error) {
	if len(path) == 1 {
		return p.appendArrayTableAt(p.root, path[0])
	}
	parent, err := p.ensureTable(path[:len(path)-1])
	if err != nil {
		return nil, err
	}
	return p.appendArrayTableAt(parent, path[len(path)-1])
}

func (p *parser) appendArrayTableAt(parent map[string]any, key string) (map[string]any, error) {
	table := make(map[string]any)
	existing, ok := parent[key]
	if !ok {
		parent[key] = []any{table}
		return table, nil
	}
	values, ok := existing.([]any)
	if !ok {
		return nil, p.errorf("array table conflicts with an existing value")
	}
	parent[key] = append(values, table)
	return table, nil
}

func (p *parser) definitionPath(path []string) []string {
	current := p.root
	result := make([]string, 0, len(path))
	for pathIndex, key := range path {
		value, ok := current[key]
		if !ok {
			return append(result, path[pathIndex:]...)
		}
		switch typed := value.(type) {
		case []any:
			index := len(typed) - 1
			result = append(result, arrayElementDefinition(key, index))
			if index >= 0 {
				current, _ = typed[index].(map[string]any)
			}
		case map[string]any:
			result = append(result, key)
			current = typed
		default:
			result = append(result, key)
		}
	}
	return result
}

func arrayElementDefinition(key string, index int) string {
	return key + "\x00#" + strconv.Itoa(index)
}

func definitionKey(path []string) string {
	var result strings.Builder
	for _, part := range path {
		result.WriteString(strconv.Itoa(len(part)))
		result.WriteByte(':')
		result.WriteString(part)
		result.WriteByte(';')
	}
	return result.String()
}

func (p *parser) arrayDefinitionPath(path []string) []string {
	if len(path) == 1 {
		return append([]string(nil), path...)
	}
	result := p.definitionPath(path[:len(path)-1])
	return append(result, path[len(path)-1])
}

func parseInteger(token string) (int64, bool) {
	sign := ""
	signless := token
	if strings.HasPrefix(signless, "+") || strings.HasPrefix(signless, "-") {
		sign = signless[:1]
		signless = signless[1:]
	}
	base := 10
	if strings.HasPrefix(signless, "0x") {
		if sign != "" {
			return 0, false
		}
		base = 16
		signless = signless[2:]
	} else if strings.HasPrefix(signless, "0o") {
		if sign != "" {
			return 0, false
		}
		base = 8
		signless = signless[2:]
	} else if strings.HasPrefix(signless, "0b") {
		if sign != "" {
			return 0, false
		}
		base = 2
		signless = signless[2:]
	} else if len(signless) > 1 && signless[0] == '0' {
		return 0, false
	}
	if !validDigitUnderscores(signless, base) {
		return 0, false
	}
	clean := strings.ReplaceAll(signless, "_", "")
	value, err := strconv.ParseInt(sign+clean, base, 64)
	return value, err == nil
}

func parseFloat(token string) (float64, bool) {
	if !validDecimalUnderscores(token) {
		return 0, false
	}
	switch token {
	case "inf", "+inf":
		return math.Inf(1), true
	case "-inf":
		return math.Inf(-1), true
	case "nan", "+nan":
		return math.NaN(), true
	case "-nan":
		return math.Copysign(math.NaN(), -1), true
	}
	if !strings.ContainsAny(token, ".eE") {
		return 0, false
	}
	clean := strings.ReplaceAll(token, "_", "")
	signless := strings.TrimPrefix(strings.TrimPrefix(clean, "+"), "-")
	if !validDecimalFloat(signless) {
		return 0, false
	}
	integerPart := strings.FieldsFunc(signless, func(r rune) bool { return r == '.' || r == 'e' || r == 'E' })[0]
	if len(integerPart) > 1 && integerPart[0] == '0' {
		return 0, false
	}
	value, err := strconv.ParseFloat(clean, 64)
	return value, err == nil
}

func validDecimalFloat(value string) bool {
	exponentIndex := strings.IndexAny(value, "eE")
	mantissa := value
	if exponentIndex >= 0 {
		if strings.IndexAny(value[exponentIndex+1:], "eE") >= 0 {
			return false
		}
		mantissa = value[:exponentIndex]
		exponent := value[exponentIndex+1:]
		if strings.HasPrefix(exponent, "+") || strings.HasPrefix(exponent, "-") {
			exponent = exponent[1:]
		}
		if !allDecimalDigits(exponent) {
			return false
		}
	}
	if strings.Count(mantissa, ".") > 1 {
		return false
	}
	if dot := strings.IndexByte(mantissa, '.'); dot >= 0 {
		return allDecimalDigits(mantissa[:dot]) && allDecimalDigits(mantissa[dot+1:])
	}
	return exponentIndex >= 0 && allDecimalDigits(mantissa)
}

func allDecimalDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

var offsetDateTimePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[Tt ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:[Zz]|[+-]\d{2}:\d{2})$`)
var localDateTimePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[Tt ]\d{2}:\d{2}:\d{2}(?:\.\d+)?$`)
var localDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
var localTimePattern = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}(?:\.\d+)?$`)

func parseDateTime(token string) (any, bool) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(token, "t", "T"), "z", "Z")
	if offsetDateTimePattern.MatchString(token) {
		if len(normalized) >= 6 {
			offset := normalized[len(normalized)-6:]
			if (offset[0] == '+' || offset[0] == '-') && offset[4:] >= "60" {
				return nil, false
			}
		}
		normalized = strings.Replace(normalized, " ", "T", 1)
		value, err := time.Parse(time.RFC3339Nano, normalized)
		return value, err == nil
	}
	if localDateTimePattern.MatchString(token) {
		normalized = strings.Replace(normalized, " ", "T", 1)
		value, err := time.ParseInLocation("2006-01-02T15:04:05.999999999", normalized, time.Local)
		return LocalDateTime{Time: value}, err == nil
	}
	if localDatePattern.MatchString(token) {
		value, err := time.ParseInLocation("2006-01-02", token, time.Local)
		return LocalDate{Time: value}, err == nil
	}
	if localTimePattern.MatchString(token) {
		value, err := time.ParseInLocation("15:04:05.999999999", token, time.Local)
		return LocalTime{Time: value}, err == nil
	}
	return nil, false
}

func validDecimalUnderscores(value string) bool {
	for index, r := range value {
		if r != '_' {
			continue
		}
		if index == 0 || index == len(value)-1 || !isDigitForBase(value[index-1], 10) || !isDigitForBase(value[index+1], 10) {
			return false
		}
	}
	return true
}

func validDigitUnderscores(value string, base int) bool {
	if value == "" || value[0] == '_' || value[len(value)-1] == '_' {
		return false
	}
	for index := range value {
		if value[index] == '_' {
			if index == 0 || index+1 >= len(value) || !isDigitForBase(value[index-1], base) || !isDigitForBase(value[index+1], base) {
				return false
			}
			continue
		}
		if !isDigitForBase(value[index], base) {
			return false
		}
	}
	return true
}

func isDigitForBase(value byte, base int) bool {
	if value >= '0' && value <= '9' {
		return int(value-'0') < base
	}
	if value >= 'a' && value <= 'f' {
		return int(value-'a')+10 < base
	}
	if value >= 'A' && value <= 'F' {
		return int(value-'A')+10 < base
	}
	return false
}

func (p *parser) skipArrayWhitespace() {
	for {
		p.skipHorizontalWhitespace()
		if p.peek() == '#' {
			p.skipComment()
		}
		if !p.isNewline() {
			return
		}
		p.consumeNewline()
	}
}

func (p *parser) skipHorizontalWhitespace() {
	for p.peek() == ' ' || p.peek() == '\t' {
		p.advance()
	}
}

func (p *parser) skipComment() {
	for !p.eof() && !p.isNewline() {
		p.advance()
	}
}

func (p *parser) consumeLineEnd() error {
	if p.eof() {
		return nil
	}
	if !p.isNewline() {
		return p.errorf("unexpected characters after expression")
	}
	p.consumeNewline()
	return nil
}

func (p *parser) consumeNewline() {
	if p.peek() == '\r' {
		p.advance()
		if p.peek() == '\n' {
			p.advance()
		}
		return
	}
	p.advance()
}

func (p *parser) isNewline() bool {
	return p.peek() == '\n' || p.peek() == '\r' && p.index+1 < len(p.source) && p.source[p.index+1] == '\n'
}

func (p *parser) hasPrefix(value string) bool {
	runes := []rune(value)
	return p.index+len(runes) <= len(p.source) && string(p.source[p.index:p.index+len(runes)]) == value
}

func (p *parser) peek() rune {
	if p.eof() {
		return 0
	}
	return p.source[p.index]
}

func (p *parser) advance() rune {
	if p.eof() {
		return 0
	}
	r := p.source[p.index]
	p.index++
	if r == '\n' {
		p.line++
		p.column = 1
	} else {
		p.column++
	}
	return r
}

func (p *parser) recalculatePosition() {
	p.line = 1
	p.column = 1
	for _, r := range p.source[:p.index] {
		if r == '\n' {
			p.line++
			p.column = 1
		} else {
			p.column++
		}
	}
}

func (p *parser) eof() bool {
	return p.index >= len(p.source)
}

func (p *parser) errorf(format string, args ...any) error {
	return &ParseError{Line: p.line, Column: p.column, Message: fmt.Sprintf(format, args...)}
}

func isBareKeyRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-'
}
