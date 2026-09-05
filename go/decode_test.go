package toml

import (
	"errors"
	"io"
	"math"
	"strings"
	"testing"
	"time"
)

func TestUnmarshalDocument(t *testing.T) {
	type Server struct {
		IP   string `toml:"ip"`
		Role string `toml:"role"`
	}
	type Owner struct {
		Name string    `toml:"name"`
		DOB  time.Time `toml:"dob"`
	}
	type Config struct {
		Title    string                    `toml:"title"`
		Enabled  bool                      `toml:"enabled"`
		Count    int                       `toml:"count"`
		Ratio    float64                   `toml:"ratio"`
		Ports    []int                     `toml:"ports"`
		Owner    *Owner                    `toml:"owner"`
		Servers  map[string]Server         `toml:"servers"`
		Products []map[string]any          `toml:"products"`
		Matrix   [2][2]int                 `toml:"matrix"`
		Ignored  string                    `toml:"-"`
		Metadata map[string]map[string]any `toml:"metadata"`
	}
	document := `
title = "TOML Example"
enabled = true
count = 1_000
ratio = 3.5
ports = [8000, 8001, 8002]
matrix = [[1, 2], [3, 4]]

[owner]
name = "Tom"
dob = 1979-05-27T07:32:00Z

[servers.alpha]
ip = "10.0.0.1"
role = "frontend"

[servers.beta]
ip = "10.0.0.2"
role = "backend"

[metadata.build]
commit = "abc"

[[products]]
name = "Hammer"
sku = 738594937

[[products]]
name = "Nail"
sku = 284758393
`
	var config Config
	if err := Unmarshal([]byte(document), &config); err != nil {
		t.Fatalf("Unmarshal() returned an unexpected error: %v", err)
	}
	if config.Title != "TOML Example" || !config.Enabled || config.Count != 1000 || config.Ratio != 3.5 {
		t.Fatalf("unexpected scalar values: %#v", config)
	}
	if config.Owner == nil || config.Owner.Name != "Tom" || config.Owner.DOB.Format(time.RFC3339) != "1979-05-27T07:32:00Z" {
		t.Fatalf("unexpected owner: %#v", config.Owner)
	}
	if len(config.Ports) != 3 || config.Ports[2] != 8002 || config.Matrix[1][1] != 4 {
		t.Fatalf("unexpected arrays: ports=%#v matrix=%#v", config.Ports, config.Matrix)
	}
	if config.Servers["alpha"].Role != "frontend" || len(config.Products) != 2 || config.Products[1]["name"] != "Nail" {
		t.Fatalf("unexpected tables: servers=%#v products=%#v", config.Servers, config.Products)
	}
}

func TestUnmarshalStringsAndKeys(t *testing.T) {
	document := `
"quoted.key" = "line\nvalue"
literal = 'C:\Users\name'
multiline = """
The quick brown \
  fox jumps."""
raw = '''
first
second'''
unicode = "\u03B1\U0001F600"
`
	var result map[string]any
	if err := Unmarshal([]byte(document), &result); err != nil {
		t.Fatalf("Unmarshal() returned an unexpected error: %v", err)
	}
	if result["quoted.key"] != "line\nvalue" {
		t.Fatalf("quoted key value = %#v", result["quoted.key"])
	}
	if result["literal"] != `C:\Users\name` || result["multiline"] != "The quick brown fox jumps." {
		t.Fatalf("unexpected strings: %#v", result)
	}
	if result["raw"] != "first\nsecond" || result["unicode"] != "α😀" {
		t.Fatalf("unexpected multiline or Unicode strings: %#v", result)
	}
}

func TestUnmarshalNumbersAndDateTimes(t *testing.T) {
	document := `
decimal = -17
hex = 0xDEAD_BEEF
octal = 0o755
binary = 0b1101
fraction = 6.626e-34
positive_infinity = inf
negative_infinity = -inf
not_a_number = nan
local_date = 1979-05-27
local_time = 07:32:00
	local_datetime = 1979-05-27T07:32:00
	offset_space = 1979-05-27 07:32:00-07:00
`
	var result map[string]any
	if err := Unmarshal([]byte(document), &result); err != nil {
		t.Fatalf("Unmarshal() returned an unexpected error: %v", err)
	}
	if result["decimal"] != int64(-17) || result["hex"] != int64(0xDEADBEEF) || result["octal"] != int64(0755) || result["binary"] != int64(13) {
		t.Fatalf("unexpected integers: %#v", result)
	}
	if !math.IsInf(result["positive_infinity"].(float64), 1) || !math.IsInf(result["negative_infinity"].(float64), -1) || !math.IsNaN(result["not_a_number"].(float64)) {
		t.Fatalf("unexpected special floats: %#v", result)
	}
	if _, ok := result["local_date"].(LocalDate); !ok {
		t.Fatalf("local_date type = %T, want LocalDate", result["local_date"])
	}
	if _, ok := result["local_time"].(LocalTime); !ok {
		t.Fatalf("local_time type = %T, want LocalTime", result["local_time"])
	}
	if _, ok := result["local_datetime"].(LocalDateTime); !ok {
		t.Fatalf("local_datetime type = %T, want LocalDateTime", result["local_datetime"])
	}
	if _, ok := result["offset_space"].(time.Time); !ok {
		t.Fatalf("offset_space type = %T, want time.Time", result["offset_space"])
	}
}

func TestDecoderDecode(t *testing.T) {
	decoder := NewDecoder(strings.NewReader("name = \"agent\"\n"))
	var value struct {
		Name string `toml:"name"`
	}
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("Decode() returned an unexpected error: %v", err)
	}
	if value.Name != "agent" {
		t.Fatalf("Name = %q, want %q", value.Name, "agent")
	}
}

func TestDecoderPropagatesReadError(t *testing.T) {
	expected := errors.New("read failed")
	decoder := NewDecoder(errorReader{err: expected})
	var value map[string]any
	if err := decoder.Decode(&value); !errors.Is(err, expected) {
		t.Fatalf("Decode() error = %v, want wrapped %v", err, expected)
	}
}

func TestUnmarshalRejectsInvalidDocuments(t *testing.T) {
	tests := []string{
		"key =\n",
		"key = 1\nkey = 2\n",
		"key = \"unterminated\n",
		"key = [1 2]\n",
		"key = { value = 1, }\n",
		"[table]\n[table]\n",
		"key = 0123\n",
		"key = 1__2\n",
		"key = unknown\n",
		"key = \"\x01\"\n",
		"key = .7\n",
		"key = 7.\n",
		"key = 1e\n",
		"key = 1\rnext = 2\n",
		"+key = 1\n",
		"key = +0x1\n",
		"fruit.apple.color = \"red\"\n[fruit]\n",
		"fruit = { apple = \"red\" }\n[fruit]\n",
	}
	for _, document := range tests {
		t.Run(strings.ReplaceAll(document, "\n", "_"), func(t *testing.T) {
			var value map[string]any
			err := Unmarshal([]byte(document), &value)
			if err == nil {
				t.Fatalf("Unmarshal(%q) error = nil, want an error", document)
			}
			var parseError *ParseError
			if !errors.As(err, &parseError) {
				t.Fatalf("Unmarshal() error = %v, want ParseError", err)
			}
			if parseError.Line <= 0 || parseError.Column <= 0 {
				t.Fatalf("ParseError location = %d:%d", parseError.Line, parseError.Column)
			}
		})
	}
}

func TestUnmarshalNestedArrayTables(t *testing.T) {
	document := `
[[fruits]]
name = "apple"
[[fruits.varieties]]
name = "red delicious"
[[fruits.varieties]]
name = "granny smith"
[[fruits]]
name = "banana"
[[fruits.varieties]]
name = "plantain"
`
	var value map[string]any
	if err := Unmarshal([]byte(document), &value); err != nil {
		t.Fatalf("Unmarshal() returned an unexpected error: %v", err)
	}
	fruits := value["fruits"].([]any)
	if len(fruits) != 2 {
		t.Fatalf("fruit count = %d, want 2", len(fruits))
	}
	firstVarieties := fruits[0].(map[string]any)["varieties"].([]any)
	secondVarieties := fruits[1].(map[string]any)["varieties"].([]any)
	if len(firstVarieties) != 2 || len(secondVarieties) != 1 {
		t.Fatalf("unexpected varieties: first=%#v second=%#v", firstVarieties, secondVarieties)
	}
}

func TestUnmarshalAllowsParentTableBeforeChildTable(t *testing.T) {
	document := "[fruit]\nname = \"apple\"\n[fruit.physical]\ncolor = \"red\"\n"
	var value map[string]any
	if err := Unmarshal([]byte(document), &value); err != nil {
		t.Fatalf("Unmarshal() returned an unexpected error: %v", err)
	}
}

func TestUnmarshalAllowsExplicitParentAfterImplicitParent(t *testing.T) {
	document := "[a.b.c]\nanswer = 42\n[a]\nbetter = 43\n"
	var value map[string]any
	if err := Unmarshal([]byte(document), &value); err != nil {
		t.Fatalf("Unmarshal() returned an unexpected error: %v", err)
	}
}

func TestUnmarshalRejectsDottedKeyExtendingExplicitTable(t *testing.T) {
	document := "[a.b.c]\nz = 9\n[a]\nb.c.t = \"invalid\"\n"
	var value map[string]any
	if err := Unmarshal([]byte(document), &value); err == nil {
		t.Fatal("Unmarshal() error = nil, want an error")
	}
}

func TestUnmarshalPreservesTerminalQuotesInMultilineStrings(t *testing.T) {
	document := "basic = \"\"\"value\"\"\"\"\nliteral = '''value''''\n"
	var value map[string]any
	if err := Unmarshal([]byte(document), &value); err != nil {
		t.Fatalf("Unmarshal() returned an unexpected error: %v", err)
	}
	if value["basic"] != `value"` || value["literal"] != "value'" {
		t.Fatalf("unexpected multiline strings: %#v", value)
	}
}

func TestUnmarshalRejectsInlineTableExtension(t *testing.T) {
	document := "value = { fruit = { name = \"apple\" }, fruit.color = \"red\" }\n"
	var value map[string]any
	if err := Unmarshal([]byte(document), &value); err == nil {
		t.Fatal("Unmarshal() error = nil, want an error")
	}
}

func FuzzUnmarshalDoesNotPanic(f *testing.F) {
	for _, document := range []string{
		"key = \"value\"\n",
		"array = [1, 2, 3]\n",
		"[table]\nkey = true\n",
		"[[products]]\nname = \"item\"\n",
		"key = \"unterminated",
	} {
		f.Add([]byte(document))
	}
	f.Fuzz(func(t *testing.T, document []byte) {
		var value map[string]any
		_ = Unmarshal(document, &value)
	})
}

func TestUnmarshalMappingValidation(t *testing.T) {
	if err := Unmarshal([]byte("key = 1"), nil); err == nil {
		t.Fatal("Unmarshal() error = nil for nil destination")
	}
	var wrong struct {
		Key bool `toml:"key"`
	}
	if err := Unmarshal([]byte("key = 1"), &wrong); err == nil {
		t.Fatal("Unmarshal() error = nil for incompatible destination")
	}
	var fixed [2]int
	if err := Unmarshal([]byte("values = [1]"), &struct {
		Values *[2]int `toml:"values"`
	}{Values: &fixed}); err == nil {
		t.Fatal("Unmarshal() error = nil for mismatched array length")
	}
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

var _ io.Reader = errorReader{}
