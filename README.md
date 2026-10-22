# TOML

Go module for decoding [TOML 1.0.0](https://toml.io/en/v1.0.0) documents.

Development and verification use Go 1.27.3. The module's minimum Go version
remains 1.26.3; `go.mod` selects the development toolchain separately.

## Module

```text
github.com/k4k3ru-hub/toml/go
```

## Decode

```go
package main

import (
	"fmt"

	toml "github.com/k4k3ru-hub/toml/go"
)

func main() {
	var config struct {
		Name    string   `toml:"name"`
		Symbols []string `toml:"symbols"`
	}

	err := toml.Unmarshal([]byte(`
name = "agent"
symbols = ["BTC/USDC", "ETH/USDC"]
`), &config)
	if err != nil {
		panic(err)
	}

	fmt.Println(config.Name)
}
```

Streaming input is supported through `Decoder`:

```go
decoder := toml.NewDecoder(reader)
err := decoder.Decode(&config)
```

The initial release is decode-only. Encoding will be implemented separately.

## Conformance

The decoder is tested against the official TOML 1.0 test suite through
`toml-test` v2.2.0. Run the pinned conformance test from the Go module:

```sh
sh scripts/test-conformance.sh
```

The test runner is downloaded only for this test command and is not a module
dependency.
